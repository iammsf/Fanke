package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"fanke/backend/internal/media"
	"fanke/backend/internal/repo"
)

const (
	maxImageUpload      = 100 << 20
	maxVideoUpload      = 1 << 30
	maxAttachmentUpload = 100 << 20
	maxUploadRequest    = maxVideoUpload + (1 << 20)
	multipartMemory     = 8 << 20
)

var errUploadTooLarge = errors.New("file too large")

func uploadLimit(ext string) int64 {
	ext = strings.ToLower(ext)
	if videoExts[ext] {
		return maxVideoUpload
	}
	if imageExts[ext] {
		return maxImageUpload
	}
	return maxAttachmentUpload
}

func hashUpload(f io.ReadSeeker, limit int64) (string, int64, error) {
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, limit+1))
	if err != nil {
		return "", n, err
	}
	if n > limit {
		return "", n, errUploadTooLarge
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", n, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

var (
	imageExts  = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".svg": true, ".avif": true}
	videoExts  = map[string]bool{".mp4": true, ".webm": true, ".mov": true}
	docExts    = map[string]bool{".pdf": true, ".zip": true, ".txt": true, ".md": true, ".csv": true}
	allowedExt = unionMaps(imageExts, videoExts, docExts)
)

func unionMaps(ms ...map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, m := range ms {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// typeAllowed 按 s3_config.allowed 判定扩展名是否可入桶。
func typeAllowed(ext, allowed string) bool {
	switch allowed {
	case "", "images":
		return imageExts[ext]
	case "all":
		return allowedExt[ext]
	default:
		for _, e := range strings.Split(allowed, ",") {
			e = strings.TrimSpace(strings.ToLower(e))
			if e != "" && "."+e == ext {
				return true
			}
		}
		return false
	}
}

func folderFor(ext string) string {
	switch {
	case imageExts[ext]:
		return "images/"
	case videoExts[ext]:
		return "videos/"
	case docExts[ext]:
		return "docs/"
	}
	return "files/"
}

func (a *App) pickStorage(ctx context.Context, pref string) (media.Storage, media.S3Config, error) {
	if pref == "" || pref == "local" {
		return a.Local, media.S3Config{}, nil
	}
	if strings.HasPrefix(pref, "s3:") {
		if !a.mediaPluginActive(ctx, "s3-storage") {
			return nil, media.S3Config{}, errors.New("S3 插件未安装、初始化或启用")
		}
		id := strings.TrimPrefix(pref, "s3:")
		s, err := a.DB.S3Settings(ctx)
		if err != nil {
			return nil, media.S3Config{}, err
		}
		for _, c := range s.Buckets {
			if c.ID == id {
				store, _, e := a.s3StoreConfig(ctx, c)
				if e != nil || !c.Enabled {
					return nil, c, errors.New("S3 bucket unavailable")
				}
				return store, c, e
			}
		}
		return nil, media.S3Config{}, errors.New("unknown S3 bucket")
	}
	if pref != "s3" {
		return nil, media.S3Config{}, errors.New("unknown storage")
	}
	ps, err := a.activeMediaPlugins(ctx)
	if err != nil {
		return nil, media.S3Config{}, err
	}
	for _, p := range ps {
		if p.provider != nil {
			return p.provider(ctx, a)
		}
	}
	return nil, media.S3Config{}, errors.New("S3 插件未安装、初始化或启用")
}

func (a *App) UploadMedia(w http.ResponseWriter, r *http.Request) {
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(15 * time.Minute))
	_ = controller.SetWriteDeadline(time.Now().Add(15 * time.Minute))
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadRequest)
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	if r.ContentLength > maxUploadRequest {
		a.fail(w, http.StatusRequestEntityTooLarge, "上传请求过大 (最大 1025MiB，含 multipart 开销)")
		return
	}
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			a.fail(w, http.StatusRequestEntityTooLarge, "上传请求过大 (最大 1025MiB，含 multipart 开销)")
		} else {
			a.fail(w, http.StatusBadRequest, "invalid multipart")
		}
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		a.fail(w, http.StatusBadRequest, "file required")
		return
	}
	defer f.Close()
	ext := strings.ToLower(path.Ext(hdr.Filename))
	if !allowedExt[ext] {
		a.fail(w, http.StatusBadRequest, "file type not allowed")
		return
	}
	limit := uploadLimit(ext)
	if hdr.Size > limit {
		a.fail(w, http.StatusRequestEntityTooLarge, "文件过大：图片及其他附件最大 100MiB，视频最大 1GiB")
		return
	}
	sha, size, err := hashUpload(f, limit)
	if errors.Is(err, errUploadTooLarge) {
		a.fail(w, http.StatusRequestEntityTooLarge, "文件过大：图片及其他附件最大 100MiB，视频最大 1GiB")
		return
	}
	if err != nil {
		a.fail(w, http.StatusBadRequest, "读取上传文件失败")
		return
	}
	var data []byte
	if imageExts[ext] {
		data, err = io.ReadAll(io.LimitReader(f, maxImageUpload+1))
		if err != nil || len(data) > maxImageUpload {
			a.fail(w, http.StatusBadRequest, "读取上传图片失败")
			return
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			a.fail(w, http.StatusBadRequest, "读取上传图片失败")
			return
		}
	}
	store, cfg, err := a.pickStorage(r.Context(), r.FormValue("storage"))
	if err != nil {
		a.fail(w, http.StatusBadGateway, "对象存储配置无效: "+err.Error())
		return
	}
	if store != a.Local && !typeAllowed(ext, cfg.Allowed) {
		a.fail(w, http.StatusBadRequest, "该桶内容策略为 "+orDefault(cfg.Allowed, "images")+",不允许 "+ext)
		return
	}
	storageName := store.Name()
	if strings.HasPrefix(r.FormValue("storage"), "s3:") {
		storageName = r.FormValue("storage")
	}
	plan := &MediaUploadContext{Filename: hdr.Filename, SHA256: sha, Size: size, Extension: ext, Storage: storageName, Source: f, Data: data}
	if err := a.dispatchLifecycle(r.Context(), BeforeMediaUpload, &LifecycleData{Media: &repo.Media{Filename: sanitizeName(plan.Filename), Size: plan.Size, SHA256: plan.SHA256, Storage: plan.Storage}}); err != nil {
		a.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	dup, err := a.DB.MediaBySHA(r.Context(), sha)
	if err != nil {
		a.failInternal(w, r, "media dedup", err)
		return
	}
	if dup != nil && ((store == a.Local && dup.Storage == "local") || (store != a.Local && recordInBucket(dup.Storage, dup.URL, dup.Key, cfg))) {
		// 仅同一目标存储复用；指定其他桶时必须真实上传并建立新记录。
		a.finishMediaUpload(r.Context(), w, dup, true, "")
		return
	}
	artifacts, warning := a.prepareMediaArtifacts(r.Context(), plan)
	// S3 按类型前缀直放，本地保留年月目录。
	var dir string
	if store != a.Local {
		dir = strings.Trim(cfg.Prefix, "/")
		if dir != "" {
			dir += "/"
		}
		if cfg.FolderByType {
			dir += folderFor(ext)
		}
	} else {
		dir = time.Now().UTC().Format("2006/01") + "/"
	}
	key := dir + sha[:16] + ext
	obj, err := store.Save(r.Context(), key, plan.Source, plan.Size, mimeOf(ext, hdr.Header.Get("Content-Type")))
	if err != nil {
		a.fail(w, http.StatusBadGateway, "save failed: "+err.Error())
		return
	}
	dw, dh := dims(data, ext)
	m := repo.Media{
		Filename: sanitizeName(hdr.Filename), Storage: storageName, Key: obj.Key,
		URL: obj.URL, MIME: obj.MIME, Size: obj.Size, Width: dw, Height: dh, SHA256: sha,
	}
	saved, saveWarning := saveMediaArtifacts(r.Context(), store, &m, artifacts)
	if saveWarning != "" {
		warning = saveWarning
	}
	id, err := a.DB.CreateMedia(r.Context(), m)
	if err != nil {
		_ = store.Delete(r.Context(), obj.Key)
		for _, s := range saved {
			_ = store.Delete(r.Context(), s.Object.Key)
		}
		a.failInternal(w, r, "create media", err)
		return
	}
	full, err := a.DB.FetchMedia(r.Context(), id)
	if err != nil {
		a.failInternal(w, r, "fetch media", err)
		return
	}
	if recordWarning := a.recordMediaArtifacts(r.Context(), store, full, saved); recordWarning != "" {
		warning = recordWarning
	}
	a.finishMediaUpload(r.Context(), w, full, false, warning)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (a *App) ListMedia(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("per"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	items, total, err := a.DB.ListMedia(r.Context(), limit, (page-1)*limit)
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (a *App) DeleteMedia(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "bad id")
		return
	}
	err = a.deleteMediaFamily(r.Context(), id)
	var rejected *lifecycleRejection
	if errors.As(err, &rejected) {
		a.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if errors.Is(err, repo.ErrNotFound) {
		a.fail(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) ServeLocalMedia(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "*")
	rc, err := a.Local.Open(r.Context(), key)
	if err != nil {
		a.fail(w, http.StatusNotFound, "not found")
		return
	}
	defer rc.Close()
	ext := strings.ToLower(path.Ext(key))
	w.Header().Set("Content-Type", mimeOf(ext, ""))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = io.Copy(w, rc)
}

// mimeOf 由扩展名决定 MIME;客户端声明的 Content-Type 只在同一大类时才采纳。
// 直接信任声明会让 .txt/.md 以 text/html 落盘并同源返回,形成存储型 XSS。
func mimeOf(ext, declared string) string {
	base := mimeForExt(ext)
	baseType := strings.TrimSpace(strings.Split(base, ";")[0])
	if baseType == "application/octet-stream" {
		return base
	}
	d := strings.ToLower(strings.TrimSpace(strings.Split(declared, ";")[0]))
	switch {
	case strings.HasPrefix(baseType, "image/") && strings.HasPrefix(d, "image/"):
		return d
	case strings.HasPrefix(baseType, "video/") && strings.HasPrefix(d, "video/"):
		return d
	case d == baseType:
		return d
	}
	return base
}

func mimeForExt(ext string) string {
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".avif":
		return "image/avif"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".md":
		return "text/markdown; charset=utf-8"
	case ".csv":
		return "text/csv; charset=utf-8"
	}
	return "application/octet-stream"
}

func dims(data []byte, ext string) (int, int) {
	if ext == ".svg" || ext == ".pdf" || ext == ".mp4" {
		return 0, 0
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

func sanitizeName(s string) string {
	// 统一分隔符,避免 Windows 客户端上传的 "..\\x" 绕过 path.Base 后进入下载 zip 的条目名。
	s = strings.ReplaceAll(s, "\\", "/")
	s = path.Base(s)
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}
