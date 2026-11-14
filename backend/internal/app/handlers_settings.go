package app

import (
	"encoding/json"
	"net/http"
)

// 公开配置白名单:前端渲染所需。
var publicSettings = []string{"site", "theme", "plugins", "nav", "comment_policy"}

func (a *App) PublicSettings(w http.ResponseWriter, r *http.Request) {
	out, err := cachedPublicJSON(r.Context(), &a.publicCache, cachePublicSettings, func() (map[string]json.RawMessage, error) {
		all, err := a.DB.AllSettings(r.Context())
		if err != nil {
			return nil, err
		}
		out := map[string]json.RawMessage{}
		for _, k := range publicSettings {
			if v, ok := all[k]; ok {
				out[k] = v
			}
		}
		// 向 site 注入管理员昵称,供页脚 $admin 使用。
		if raw, ok := out["site"]; ok {
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				return nil, err
			}
			name, err := a.DB.AdminDisplayName(r.Context())
			if err != nil {
				return nil, err
			}
			if m != nil {
				m["admin_name"] = name
				b, err := json.Marshal(m)
				if err != nil {
					return nil, err
				}
				out["site"] = json.RawMessage(b)
			}
		}
		return out, nil
	})
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSONCached(w, r, http.StatusOK, out)
}

func (a *App) AllSettings(w http.ResponseWriter, r *http.Request) {
	all, err := a.DB.AllSettings(r.Context())
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "query failed")
		return
	}
	delete(all, "s3_buckets")
	delete(all, "s3_config")
	delete(all, "r2_config")
	writeJSON(w, http.StatusOK, all)
}

// PutSetting 单个键更新,body 即该键的 JSON 值。
func (a *App) PutSetting(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		a.fail(w, http.StatusBadRequest, "key required")
		return
	}
	if key == "s3_config" || key == "r2_config" || key == "s3_buckets" {
		a.fail(w, http.StatusForbidden, "请使用管理员专用对象存储配置接口")
		return
	}
	var v json.RawMessage
	if err := decodeJSON(r, &v); err != nil {
		a.fail(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := a.DB.PutSetting(r.Context(), key, v); err != nil {
		a.fail(w, http.StatusInternalServerError, "save failed")
		return
	}
	a.publicCache.clear()
	w.WriteHeader(http.StatusNoContent)
}
