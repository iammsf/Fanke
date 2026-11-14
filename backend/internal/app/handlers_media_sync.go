package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"fanke/backend/internal/media"
	"fanke/backend/internal/repo"
)

const (
	syncScanMax = 5000     // 单次扫描返回的对象上限,超出置 truncated=true
	syncHashMax = 32 << 20 // 统计 sha256 / 图片尺寸的内存上限,超出则只登记元信息
)

type syncObject struct {
	Key          string    `json:"key"`
	Filename     string    `json:"filename"`
	Size         int64     `json:"size"`
	MIME         string    `json:"mime"`
	URL          string    `json:"url"`
	Allowed      bool      `json:"allowed"`   // 是否符合当前桶内容策略(allowed),false 说明前台大概率不让直接访问
	RefPosts     int       `json:"ref_posts"` // 被文章正文直接引用的篇数(删桶对象前必须提示,防误删在用的文件)
	LastModified time.Time `json:"last_modified"`
}

type syncRecord struct {
	ID        int64     `json:"id"`
	Filename  string    `json:"filename"`
	Key       string    `json:"key"`
	URL       string    `json:"url"`
	MIME      string    `json:"mime"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	RefPosts  int       `json:"ref_posts"` // 引用该媒体的文章数(删除前风险提示)
}

// SyncScan 只读对账:桶内对象 ↔ media 表记录,返回差异清单(不写任何数据)。
//   - orphans 桶里有对象、但媒体库没有任何记录(多为直接从 S3 面板/其他工具上传,或上传中断留下的碎片)
//   - missing 媒体库里 storage=s3 的记录、但桶里对象已不存在(多为桶内对象被误删)
func (a *App) SyncScan(w http.ResponseWriter, r *http.Request) {
	store, cfg, err := a.syncStore(r.Context(), r.URL.Query().Get("bucket_id"), r.URL.Query().Get("storage"))
	if err != nil || store == nil {
		a.fail(w, http.StatusBadRequest, "对象存储未配置或不可用")
		return
	}
	objs, truncated, err := media.ListObjects(r.Context(), cfg, syncScanMax)
	if err != nil {
		a.fail(w, http.StatusBadGateway, "列出桶内对象失败: "+err.Error())
		return
	}
	recs, err := a.DB.AllMedia(r.Context())
	if err != nil {
		a.failInternal(w, r, "list media", err)
		return
	}

	known := make(map[string]bool, len(recs)) // 任意 storage 的 key 都算「媒体库里有」
	localCount := 0
	for _, m := range recs {
		if recordInBucket(m.Storage, m.URL, m.Key, cfg) {
			known[m.Key] = true
		}
		if m.Storage == "local" {
			localCount++
		}
	}

	variants, err := a.DB.MediaVariants(r.Context(), 0)
	if err != nil {
		a.failInternal(w, r, "list variants", err)
		return
	}
	for _, v := range variants {
		if recordInBucket(v.Storage, v.URL, v.Key, cfg) {
			known[v.Key] = true
		}
	}
	inBucket := make(map[string]bool, len(objs))
	base := strings.TrimRight(cfg.PublicBase, "/")
	orphans := []syncObject{}
	for _, o := range objs {
		inBucket[o.Key] = true
		if known[o.Key] || strings.HasPrefix(o.Key, media.VariantPrefix) {
			continue
		}
		ext := strings.ToLower(path.Ext(o.Key))
		url := base + "/" + o.Key
		refs, _ := a.DB.MediaURLRefPosts(r.Context(), url)
		orphans = append(orphans, syncObject{
			Key: o.Key, Filename: path.Base(o.Key), Size: o.Size, MIME: mimeOf(ext, ""),
			URL: url, Allowed: typeAllowed(ext, cfg.Allowed), RefPosts: refs, LastModified: o.LastModified,
		})
	}
	sort.SliceStable(orphans, func(i, j int) bool { return orphans[i].LastModified.After(orphans[j].LastModified) })

	missing := []syncRecord{}
	other := []syncRecord{} // 记录指向别的桶/域名(历史配置),不参与本次对账,也不提供任何删除动作
	s3Count := 0
	for _, m := range recs {
		if m.Storage != "s3" && !strings.HasPrefix(m.Storage, "s3:") {
			continue
		}
		s3Count++
		// 只有 URL 落在「当前配置桶」下的记录才可与本次列出的对象比对;
		// 否则(例如曾用别的桶/域名)会把正常的图误报成"悬空",绝不能给删除入口。
		if !recordInBucket(m.Storage, m.URL, m.Key, cfg) {
			other = append(other, syncRecord{
				ID: m.ID, Filename: m.Filename, Key: m.Key, URL: m.URL, MIME: m.MIME,
				Size: m.Size, CreatedAt: m.CreatedAt,
			})
			continue
		}
		if truncated || inBucket[m.Key] {
			continue
		}
		refs, _ := a.DB.MediaRefPosts(r.Context(), m.ID, m.URL)
		missing = append(missing, syncRecord{
			ID: m.ID, Filename: m.Filename, Key: m.Key, URL: m.URL, MIME: m.MIME,
			Size: m.Size, CreatedAt: m.CreatedAt, RefPosts: refs,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"bucket_id":   cfg.ID,
		"storage":     "s3:" + cfg.ID,
		"bucket":      cfg.Bucket,
		"public_base": cfg.PublicBase,
		"allowed":     cfg.Allowed,
		"totals": map[string]any{
			"bucket_objects":   len(objs),
			"bucket_truncated": truncated,
			"records":          len(recs),
			"s3_records":       s3Count,
			"local_records":    localCount,
			"orphans":          len(orphans),
			"missing":          len(missing),
			"other_source":     len(other),
		},
		"orphans":      orphans,
		"missing":      missing,
		"other_source": other,
	})
}

func syncBucketID(id, storage string) (string, error) {
	id, storage = strings.TrimSpace(id), strings.TrimSpace(storage)
	if storage == "" || storage == "s3" {
		return id, nil
	}
	if !strings.HasPrefix(storage, "s3:") || strings.TrimPrefix(storage, "s3:") == "" {
		return "", fmt.Errorf("invalid storage selector")
	}
	selected := strings.TrimPrefix(storage, "s3:")
	if id != "" && id != selected {
		return "", fmt.Errorf("conflicting bucket selectors")
	}
	return selected, nil
}

func (a *App) syncStore(ctx context.Context, id, storage string) (media.Storage, media.S3Config, error) {
	id, err := syncBucketID(id, storage)
	if err != nil {
		return nil, media.S3Config{}, err
	}
	s, err := a.DB.S3Settings(ctx)
	if err != nil {
		return nil, media.S3Config{}, err
	}
	cfg, err := s.SelectBucket(id)
	if err != nil {
		return nil, cfg, err
	}
	return a.s3StoreConfig(ctx, cfg)
}

func recordInBucket(storage, url, key string, cfg media.S3Config) bool {
	return (storage == "s3" || storage == "s3:"+cfg.ID) && belongsToBucket(url, key, cfg.PublicBase)
}

type syncApplyReq struct {
	BucketID         string   `json:"bucket_id"`
	Storage          string   `json:"storage"`
	ImportKeys       []string `json:"import_keys"`        // 登记入库的桶内孤儿 key
	DeleteRecordIDs  []int64  `json:"delete_record_ids"`  // 删除的悬空记录 id
	DeleteObjectKeys []string `json:"delete_object_keys"` // 从桶里删除的对象 key
}

// SyncApply 应用勾选的修复动作(逐项容错,返回失败明细):
//  1. import_keys       桶内对象 HEAD 校验存在 → GET 流式算 sha256 与图片尺寸 → 登记为 s3 记录
//  2. delete_record_ids 删除悬空记录(不动桶,桶里本来就没有)
//  3. delete_object_keys 从桶里删除对象(必须由前端逐项勾选并二次确认后才下发)
func (a *App) SyncApply(w http.ResponseWriter, r *http.Request) {
	var req syncApplyReq
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, http.StatusBadRequest, "invalid body")
		return
	}
	if len(req.ImportKeys)+len(req.DeleteRecordIDs)+len(req.DeleteObjectKeys) == 0 {
		a.fail(w, http.StatusBadRequest, "nothing to do")
		return
	}
	const maxSyncAction = 2000
	if len(req.ImportKeys) > maxSyncAction || len(req.DeleteRecordIDs) > maxSyncAction || len(req.DeleteObjectKeys) > maxSyncAction {
		a.fail(w, http.StatusBadRequest, "单次同步最多处理 2000 项")
		return
	}
	// 导入/删对象都要对象存储可用;仅删记录时可不需要(但记录本就是 s3 记录,统一在这里校验)
	if req.BucketID == "" {
		req.BucketID = r.URL.Query().Get("bucket_id")
	}
	if req.Storage == "" {
		req.Storage = r.URL.Query().Get("storage")
	}
	store, cfg, err := a.syncStore(r.Context(), req.BucketID, req.Storage)
	if err != nil || store == nil {
		a.fail(w, http.StatusBadRequest, "对象存储未配置或不可用")
		return
	}

	imported, duplicated, recDeleted, objDeleted := 0, 0, 0, 0
	failed := []string{}
	base := strings.TrimRight(cfg.PublicBase, "/")
	recs, err := a.DB.AllMedia(r.Context())
	if err != nil {
		a.failInternal(w, r, "list media", err)
		return
	}
	variants, err := a.DB.MediaVariants(r.Context(), 0)
	if err != nil {
		a.failInternal(w, r, "list variants", err)
		return
	}
	protected := map[string]bool{}
	for _, m := range recs {
		if recordInBucket(m.Storage, m.URL, m.Key, cfg) {
			protected[m.Key] = true
		}
	}
	for _, v := range variants {
		if recordInBucket(v.Storage, v.URL, v.Key, cfg) {
			protected[v.Key] = true
		}
	}

	if len(req.ImportKeys) > 0 && !a.requireS3Plugin(w, r) {
		return
	}
	if len(req.ImportKeys) > 0 {
		for _, key := range req.ImportKeys {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			if strings.HasPrefix(key, media.VariantPrefix) {
				failed = append(failed, key+": 派生文件不能导入为原图")
				continue
			}
			if protected[key] {
				duplicated++ // 期间已被别处登记(或本地同 key 记录存在)
				continue
			}
			size, ctype, err := media.StatObject(r.Context(), cfg, key)
			if err != nil {
				failed = append(failed, key+": 桶内不存在或不可读")
				continue
			}
			ext := strings.ToLower(path.Ext(key))
			sha, dw, dh := syncHashObject(r.Context(), store, key, ext)
			m := repo.Media{
				Filename: sanitizeName(path.Base(key)), Storage: "s3:" + cfg.ID, Key: key,
				URL: base + "/" + key, MIME: mimeOf(ext, ctype), Size: size, Width: dw, Height: dh, SHA256: sha,
			}
			if _, err := a.DB.CreateMedia(r.Context(), m); err != nil {
				failed = append(failed, key+": 写入记录失败("+err.Error()+")")
				continue
			}
			protected[key] = true
			imported++
		}
	}

	var listed map[string]bool
	if len(req.DeleteRecordIDs) > 0 {
		objects, truncated, err := media.ListObjects(r.Context(), cfg, 10000)
		if err == nil && !truncated {
			listed = map[string]bool{}
			for _, o := range objects {
				listed[o.Key] = true
			}
		}
	}
	for _, id := range req.DeleteRecordIDs {
		m, err := a.DB.MediaByID(r.Context(), id)
		if err != nil {
			failed = append(failed, fmt.Sprintf("#%d: %v", id, err))
			continue
		}
		// 双保险:只允许删除「当前配置桶」下的记录 —— 历史配置指向别的桶/域名的正常记录
		// 绝不能被这个接口删掉,否则会把好好的封面从媒体库里抹掉。
		if !recordInBucket(m.Storage, m.URL, m.Key, cfg) {
			failed = append(failed, fmt.Sprintf("#%d: 不属于当前桶(%s),已跳过", id, cfg.Bucket))
			continue
		}
		if listed == nil || listed[m.Key] {
			failed = append(failed, fmt.Sprintf("#%d: 无法确认对象缺失或对象仍存在，已跳过", id))
			continue
		}
		refs, err := a.DB.MediaRefPosts(r.Context(), m.ID, m.URL)
		if err != nil || refs > 0 {
			failed = append(failed, fmt.Sprintf("#%d: 引用检查失败或仍被文章引用", id))
			continue
		}
		hasVariants := false
		for _, v := range variants {
			if v.MediaID == id {
				hasVariants = true
				break
			}
		}
		if hasVariants {
			failed = append(failed, fmt.Sprintf("#%d: 存在派生文件，请通过媒体库删除原图", id))
			continue
		}
		data := &LifecycleData{Media: m}
		if err := a.dispatchLifecycle(r.Context(), BeforeMediaDelete, data); err != nil {
			failed = append(failed, fmt.Sprintf("#%d: 删除被拒绝", id))
			continue
		}
		if err := a.purgeMediaCache(r.Context(), m); err != nil {
			failed = append(failed, fmt.Sprintf("#%d: 清理缓存失败", id))
			continue
		}
		if _, err := a.DB.DeleteMedia(r.Context(), id); err != nil {
			failed = append(failed, fmt.Sprintf("#%d: 删除记录失败", id))
			continue
		}
		_ = a.dispatchLifecycle(r.Context(), AfterMediaDeleted, data)
		recDeleted++
	}

	for _, key := range req.DeleteObjectKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if protected[key] || strings.HasPrefix(key, media.VariantPrefix) {
			failed = append(failed, key+": 已登记媒体或派生文件，请通过媒体库删除原图")
			continue
		}
		refs, err := a.DB.MediaURLRefPosts(r.Context(), base+"/"+key)
		if err != nil || refs > 0 {
			failed = append(failed, key+": 引用检查失败或仍被文章引用")
			continue
		}
		if err := store.Delete(r.Context(), key); err != nil {
			failed = append(failed, key+": 删除对象失败("+err.Error()+")")
			continue
		}
		objDeleted++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"imported": imported, "skipped_duplicate": duplicated,
		"records_deleted": recDeleted, "objects_deleted": objDeleted,
		"failed": failed,
	})
}

// CacheControlBackfill 给当前桶内缺少 Cache-Control 的对象补写长缓存头(幂等,可重复调用)。
//
// 背景:历史上传的对象没带缓存头,浏览器与 CDN 每次访问都要回源校验,Lighthouse 把这类
// 重复传输单独列为一项可优化点(实测主页 602 KiB / 文章页 879 KiB)。
// 做法是 self-copy + MetadataDirective=REPLACE,只改元信息,对象内容与 ETag 不变。
func (a *App) CacheControlBackfill(w http.ResponseWriter, r *http.Request) {
	_, cfg, err := a.syncStore(r.Context(), r.URL.Query().Get("bucket_id"), r.URL.Query().Get("storage"))
	if err != nil || cfg.Bucket == "" {
		a.fail(w, http.StatusBadRequest, "对象存储未配置或不可用")
		return
	}
	objs, truncated, err := media.ListObjects(r.Context(), cfg, 2000)
	if err != nil {
		a.fail(w, http.StatusBadGateway, "列出桶内对象失败: "+err.Error())
		return
	}
	updated, skipped := 0, 0
	failed := []string{}
	for _, o := range objs {
		cur, err := media.ObjectCacheControl(r.Context(), cfg, o.Key)
		if err != nil {
			failed = append(failed, o.Key+": 读取元信息失败")
			continue
		}
		if strings.Contains(cur, "immutable") || strings.Contains(cur, "max-age=") {
			skipped++ // 已有长缓存策略,不动
			continue
		}
		if err := media.SetCacheControl(r.Context(), cfg, o.Key, media.ImmutableCacheControl); err != nil {
			failed = append(failed, o.Key+": "+err.Error())
			continue
		}
		updated++
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bucket": cfg.Bucket, "objects": len(objs), "truncated": truncated,
		"updated": updated, "skipped": skipped, "failed": failed,
	})
}

// syncHashObject 下载一次对象:流式算 sha256,并把前 syncHashMax 字节留在内存里供图片尺寸解码。
// 超过上限(大视频等)时放弃哈希与尺寸,只登记元信息,避免把大文件读进内存。
func syncHashObject(ctx context.Context, store media.Storage, key, ext string) (string, int, int) {
	rc, err := store.Open(ctx, key)
	if err != nil {
		return "", 0, 0
	}
	defer rc.Close()
	var buf bytes.Buffer
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(h, &buf), io.LimitReader(rc, syncHashMax+1))
	if err != nil || n > syncHashMax {
		return "", 0, 0
	}
	dw, dh := dims(buf.Bytes(), ext)
	return hex.EncodeToString(h.Sum(nil)), dw, dh
}
