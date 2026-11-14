package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// publicCacheControl 公开读接口的缓存策略:
// 浏览器缓存 30 秒,期间过期后可用旧副本回源(stale-while-revalidate),避免每次导航都重拉。
const publicCacheControl = "public, max-age=30, stale-while-revalidate=120"

// writeJSONCached 用于公开 GET:附带 ETag,命中 If-None-Match 时返回 304。
// 管理端与鉴权接口仍走 writeJSON(no-store),不要混用。
func writeJSONCached(w http.ResponseWriter, r *http.Request, code int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp{Error: "encode failed"})
		return
	}
	sum := sha256.Sum256(b)
	etag := `"` + hex.EncodeToString(sum[:10]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", publicCacheControl)
	if inm := r.Header.Get("If-None-Match"); inm != "" && strings.Contains(inm, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(b)
}
