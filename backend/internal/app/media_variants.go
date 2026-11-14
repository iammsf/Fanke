package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"fanke/backend/internal/media"
	"fanke/backend/internal/repo"
)

func processImageUpload(ctx context.Context, a *App, u *MediaUploadContext) ([]MediaArtifact, error) {
	var cfg ImageProcessingSettings
	raw, err := a.DB.GetSetting(ctx, "media_image_upload")
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if !cfg.Enabled || !media.Thumbable(u.Filename) {
		return nil, nil
	}
	cfg = normalizeImageProcessing(cfg)
	if u.Storage == "s3" || strings.HasPrefix(u.Storage, "s3:") {
		_, c, err := a.syncStore(ctx, "", u.Storage)
		if err != nil {
			return nil, err
		}
		if !typeAllowed(".webp", c.Allowed) {
			return nil, fmt.Errorf("桶策略不允许 WebP")
		}
	}
	return createImageArtifacts(ctx, a.Thumbs, u, cfg)
}

func createImageArtifacts(ctx context.Context, thumbs *media.Thumbnailer, u *MediaUploadContext, cfg ImageProcessingSettings) ([]MediaArtifact, error) {
	cfg = normalizeImageProcessing(cfg)
	dimensions, err := media.StaticImageConfig(u.Data)
	if err != nil {
		return nil, err
	}
	if thumbs == nil {
		return nil, fmt.Errorf("压缩服务不可用")
	}
	out := []MediaArtifact{}
	for _, width := range configuredVariantWidths(cfg.Widths, dimensions.Width) {
		file, err := thumbs.GetWithQuality(ctx, "upload|"+u.SHA256, width, cfg.Quality, func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(u.Data)), nil })
		if err != nil {
			return out, err
		}
		height := dimensions.Height * width / dimensions.Width
		if height < 1 {
			height = 1
		}
		out = append(out, MediaArtifact{File: file, Width: width, Height: height})
	}
	return out, nil
}

func configuredVariantWidths(widths []int, source int) []int {
	out := make([]int, 0, len(widths))
	for _, width := range widths {
		if source < width {
			width = source
		}
		if width > 0 && (len(out) == 0 || out[len(out)-1] != width) {
			out = append(out, width)
		}
		if width == source {
			break
		}
	}
	return out
}

type savedMediaArtifact struct {
	Artifact MediaArtifact
	Object   media.Object
}

func saveMediaArtifacts(ctx context.Context, store media.Storage, m *repo.Media, artifacts []MediaArtifact) ([]savedMediaArtifact, string) {
	out := []savedMediaArtifact{}
	for _, artifact := range artifacts {
		f, err := os.Open(artifact.File)
		if err != nil {
			return out, "原图已保留，读取派生失败"
		}
		stat, err := f.Stat()
		if err != nil {
			f.Close()
			return out, "原图已保留，读取派生失败"
		}
		obj, err := store.Save(ctx, media.VariantKey(m.Filename, m.SHA256, m.ID, artifact.Width), f, stat.Size(), "image/webp")
		f.Close()
		if err != nil {
			return out, "原图已保留，保存派生失败: " + err.Error()
		}
		out = append(out, savedMediaArtifact{artifact, obj})
	}
	return out, ""
}
func (a *App) recordMediaArtifacts(ctx context.Context, store media.Storage, m *repo.Media, saved []savedMediaArtifact) string {
	for i, s := range saved {
		err := a.DB.CreateMediaVariant(ctx, repo.MediaVariant{MediaID: m.ID, Width: s.Artifact.Width, Height: s.Artifact.Height, Storage: m.Storage, Key: s.Object.Key, URL: s.Object.URL, Size: s.Object.Size})
		if err != nil {
			for _, left := range saved[i:] {
				_ = store.Delete(ctx, left.Object.Key)
			}
			return "原图已保留，派生入库失败"
		}
	}
	return ""
}

// finishMediaUpload 只在原图成功且派生处理结束（含跳过或部分失败）后调用。
func (a *App) finishMediaUpload(ctx context.Context, w http.ResponseWriter, m *repo.Media, reused bool, warning string) {
	variants, err := a.DB.MediaVariants(ctx, m.ID)
	if err != nil {
		log.Printf("lifecycle %s media %d variants: %v", AfterMediaUploaded, m.ID, err)
	}
	_ = a.dispatchLifecycle(ctx, AfterMediaUploaded, &LifecycleData{Media: m, Variants: variants, Reused: reused})
	uploadMediaResponse(w, m, warning)
}

func uploadMediaResponse(w http.ResponseWriter, m *repo.Media, warning string) {
	if warning == "" {
		writeJSON(w, http.StatusOK, m)
		return
	}
	log.Printf("media upload id=%d: %s", m.ID, warning)
	writeJSON(w, http.StatusOK, struct {
		*repo.Media
		Warning string `json:"warning"`
	}{m, warning})
}

func belongsToBucket(url, key, base string) bool {
	return base != "" && key != "" && url == strings.TrimRight(base, "/")+"/"+key
}

func (a *App) mediaStorage(ctx context.Context, storage, key, url string) (media.Storage, error) {
	if storage == "local" {
		return a.Local, nil
	}
	if storage != "s3" && !strings.HasPrefix(storage, "s3:") {
		return nil, fmt.Errorf("unknown storage %s", storage)
	}
	settings, err := a.DB.S3Settings(ctx)
	if err != nil {
		return nil, err
	}
	for _, cfg := range settings.Buckets {
		if strings.HasPrefix(storage, "s3:") && cfg.ID != strings.TrimPrefix(storage, "s3:") {
			continue
		}
		if cfg.ID == "" || !belongsToBucket(url, key, cfg.PublicBase) {
			continue
		}
		s, _, err := a.s3StoreConfig(ctx, cfg)
		if err == nil && s != nil {
			return s, nil
		}
	}
	return nil, fmt.Errorf("media does not belong to configured S3 bucket")
}

// Delete files before the DB row: on partial failure all relationships remain retryable.
func deleteMediaFiles(ctx context.Context, store media.Storage, key string, variants []repo.MediaVariant) error {
	for _, v := range variants {
		if err := store.Delete(ctx, v.Key); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("delete variant %s: %w", v.Key, err)
		}
	}
	if err := store.Delete(ctx, key); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (a *App) purgeMediaCache(ctx context.Context, m *repo.Media) error {
	if a.Thumbs == nil {
		return nil
	}
	ids := []string{"local|" + m.Key, "ext|" + m.URL, "upload|" + m.SHA256}
	if id, _, ok := a.thumbSource(ctx, m.URL); ok {
		ids = append(ids, id)
	}
	for _, id := range ids {
		if err := a.Thumbs.Purge(id); err != nil {
			return err
		}
	}
	return nil
}

type lifecycleRejection struct{ err error }

func (e *lifecycleRejection) Error() string { return e.err.Error() }
func (e *lifecycleRejection) Unwrap() error { return e.err }

func (a *App) deleteMediaFamily(ctx context.Context, id int64) error {
	m, err := a.DB.MediaByID(ctx, id)
	if err != nil {
		return err
	}
	variants, err := a.DB.MediaVariants(ctx, id)
	if err != nil {
		return err
	}
	data := &LifecycleData{Media: m, Variants: variants}
	if err := a.dispatchLifecycle(ctx, BeforeMediaDelete, data); err != nil {
		return &lifecycleRejection{err}
	}
	store, err := a.mediaStorage(ctx, m.Storage, m.Key, m.URL)
	if err != nil {
		return err
	}
	for _, v := range variants {
		if v.Storage != m.Storage {
			return fmt.Errorf("variant storage mismatch")
		}
		if _, err := a.mediaStorage(ctx, v.Storage, v.Key, v.URL); err != nil {
			return err
		}
	}
	if err := a.purgeMediaCache(ctx, m); err != nil {
		return err
	}
	if err := deleteMediaFiles(ctx, store, m.Key, variants); err != nil {
		return err
	}
	if _, err = a.DB.DeleteMedia(ctx, id); err != nil {
		return err
	}
	_ = a.dispatchLifecycle(ctx, AfterMediaDeleted, data)
	return nil
}

func selectMediaVariant(variants []repo.MediaVariant, width int) *repo.MediaVariant {
	var best *repo.MediaVariant
	for i := range variants {
		v := &variants[i]
		if best == nil || (best.Width < width && v.Width > best.Width) || (v.Width >= width && v.Width < best.Width) {
			best = v
		}
	}
	return best
}

func (a *App) serveMediaVariant(w http.ResponseWriter, r *http.Request, src string, width int) bool {
	if a.DB == nil {
		return false
	}
	m, err := a.DB.MediaByURL(r.Context(), src)
	if err != nil || m == nil {
		return false
	}
	variants, err := a.DB.MediaVariants(r.Context(), m.ID)
	if err != nil {
		return false
	}
	v := selectMediaVariant(variants, width)
	if v == nil {
		return false
	}
	store, err := a.mediaStorage(r.Context(), v.Storage, v.Key, v.URL)
	if err != nil {
		return false
	}
	rc, err := store.Open(r.Context(), v.Key)
	if err != nil {
		return false
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Cache-Control", media.ImmutableCacheControl)
	w.Header().Set("Vary", "Accept")
	w.Header().Set("X-Thumb", "variant")
	_, err = io.Copy(w, rc)
	if err != nil {
		log.Printf("serve variant id=%d: %v", m.ID, err)
	}
	return true
}
