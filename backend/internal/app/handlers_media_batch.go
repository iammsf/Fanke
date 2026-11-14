package app

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"fanke/backend/internal/media"
)

// maxBatchMedia 单次批量操作的媒体数量上限,防止一次请求拖垮数据库/对象存储。
const maxBatchMedia = 500

type batchIDsReq struct {
	IDs     []int64 `json:"ids"`
	Storage string  `json:"storage"`
}

func (r batchIDsReq) tooMany() bool { return len(r.IDs) > maxBatchMedia }

// BatchDeleteMedia 批量删除媒体(记录+文件),逐项容错,返回失败明细。
func (a *App) BatchDeleteMedia(w http.ResponseWriter, r *http.Request) {
	var req batchIDsReq
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 {
		a.fail(w, http.StatusBadRequest, "ids required")
		return
	}
	if req.tooMany() {
		a.fail(w, http.StatusBadRequest, "一次最多操作 500 个媒体")
		return
	}
	ok := 0
	failed := []string{}
	for _, id := range req.IDs {
		if err := a.deleteMediaFamily(r.Context(), id); err != nil {
			failed = append(failed, fmt.Sprintf("#%d: %v", id, err))
			continue
		}
		ok++
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "failed": failed})
}

// BatchTransferS3 批量把 local 媒体转移到 S3;已是 s3 的跳过,失败的保留原样。
func (a *App) BatchTransferS3(w http.ResponseWriter, r *http.Request) {
	var req batchIDsReq
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 {
		a.fail(w, http.StatusBadRequest, "ids required")
		return
	}
	if req.tooMany() {
		a.fail(w, http.StatusBadRequest, "一次最多操作 500 个媒体")
		return
	}
	pref := req.Storage
	if pref == "" {
		pref = "s3"
	}
	store, cfg, err := a.pickStorage(r.Context(), pref)
	if err != nil || store == nil {
		a.fail(w, http.StatusBadRequest, "对象存储未配置或不可用")
		return
	}
	ok, skipped := 0, 0
	var rewritten, extRewritten int64 // 文章正文 / 插件表 链接替换总数
	failed := []string{}
	for _, id := range req.IDs {
		m, err := a.DB.MediaByID(r.Context(), id)
		if err != nil {
			failed = append(failed, fmt.Sprintf("#%d: %v", id, err))
			continue
		}
		if m.Storage != "local" {
			skipped++
			continue
		}
		ext := strings.ToLower(path.Ext(m.Key))
		if !typeAllowed(ext, cfg.Allowed) {
			failed = append(failed, fmt.Sprintf("%s: 桶策略不允许 %s", m.Filename, ext))
			continue
		}
		rc, err := a.Local.Open(r.Context(), m.Key)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: 读本地失败 %v", m.Filename, err))
			continue
		}
		destKey := strings.Trim(cfg.Prefix, "/")
		if destKey != "" {
			destKey += "/"
		}
		destKey += m.Key
		obj, err := store.Save(r.Context(), destKey, rc, m.Size, m.MIME)
		rc.Close()
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: 上传 S3 失败 %v", m.Filename, err))
			continue
		}
		variants, err := a.DB.MediaVariants(r.Context(), m.ID)
		if err == nil {
			for i := range variants {
				v := &variants[i]
				if v.Storage != "local" || !typeAllowed(".webp", cfg.Allowed) {
					err = fmt.Errorf("派生存储或桶策略不匹配")
					break
				}
				var rc io.ReadCloser
				rc, err = a.Local.Open(r.Context(), v.Key)
				if err != nil {
					break
				}
				var vo media.Object
				vo, err = store.Save(r.Context(), v.Key, rc, v.Size, "image/webp")
				rc.Close()
				if err != nil {
					break
				}
				v.URL = vo.URL
			}
		}
		if err == nil {
			err = a.DB.TransferMediaFamily(r.Context(), m.ID, store.Name(), obj.Key, obj.URL, variants)
		}
		if err != nil {
			// Local records and files remain intact; reserved-prefix S3 leftovers are never imported as originals.
			failed = append(failed, fmt.Sprintf("%s: 转移媒体及派生失败 %v", m.Filename, err))
			continue
		}
		if err := a.purgeMediaCache(r.Context(), m); err != nil {
			failed = append(failed, fmt.Sprintf("%s: 清缓存失败 %v", m.Filename, err))
		}
		// 把文章正文(md+html)里引用的旧本地 URL 统一换成 S3 回调地址;失败不回滚转移,仅提示
		if n, rerr := a.DB.ReplaceMediaURL(r.Context(), m.URL, obj.URL); rerr != nil {
			failed = append(failed, fmt.Sprintf("%s: 文章链接替换失败 %v", m.Filename, rerr))
		} else {
			rewritten += n
		}
		// 插件数据表(ext_data_*)里的旧 URL(如 image-focus 以 URL 为键的焦点记录)同步改写
		if n, rerr := a.DB.ReplaceMediaURLInExtensions(r.Context(), m.URL, obj.URL); rerr != nil {
			failed = append(failed, fmt.Sprintf("%s: 插件记录替换失败 %v", m.Filename, rerr))
		} else {
			extRewritten += n
		}
		if err := deleteMediaFiles(r.Context(), a.Local, m.Key, variants); err != nil {
			failed = append(failed, fmt.Sprintf("%s: S3 转移完成，本地清理失败 %v", m.Filename, err))
		}
		ok++
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "skipped": skipped, "failed": failed, "rewritten": rewritten, "ext_rewritten": extRewritten})
}

// BatchDownloadMedia 批量打包下载(zip 流式输出,local/s3 均可)。
func (a *App) BatchDownloadMedia(w http.ResponseWriter, r *http.Request) {
	var req batchIDsReq
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 {
		a.fail(w, http.StatusBadRequest, "ids required")
		return
	}
	if req.tooMany() {
		a.fail(w, http.StatusBadRequest, "一次最多打包 500 个媒体")
		return
	}
	// 打包大文件可能远超服务端默认 WriteTimeout,单独放宽本次写超时。
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(15 * time.Minute))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="media.zip"`)
	zw := zip.NewWriter(w)
	failed := []string{}
	for _, id := range req.IDs {
		m, err := a.DB.MediaByID(r.Context(), id)
		if err != nil {
			failed = append(failed, fmt.Sprintf("#%d: %v", id, err))
			continue
		}
		store, err := a.mediaStorage(r.Context(), m.Storage, m.Key, m.URL)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: 存储不可用", m.Filename))
			continue
		}
		rc, err := store.Open(r.Context(), m.Key)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", m.Filename, err))
			continue
		}
		entry := fmt.Sprintf("%d_%s", m.ID, m.Filename) // 防重名
		fw, err := zw.Create(entry)
		if err == nil {
			_, err = io.Copy(fw, rc)
		}
		rc.Close()
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: 打包失败 %v", m.Filename, err))
		}
	}
	if len(failed) > 0 {
		if fw, err := zw.Create("_errors.txt"); err == nil {
			_, _ = fw.Write([]byte(strings.Join(failed, "\n")))
		}
	}
	_ = zw.Close()
}
