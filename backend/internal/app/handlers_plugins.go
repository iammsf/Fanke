package app

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"fanke/backend/internal/repo"
)

// ListPluginsPublic 公开:启用的自定义插件(前端注入用)。
func (a *App) ListPluginsPublic(w http.ResponseWriter, r *http.Request) {
	items, err := cachedPublicJSON(r.Context(), &a.publicCache, cachePublicPlugins, func() ([]repo.Plugin, error) {
		return a.DB.ListPlugins(r.Context(), true)
	})
	if err != nil {
		a.failInternal(w, r, "list plugins", err)
		return
	}
	writeJSONCached(w, r, http.StatusOK, items)
}

func (a *App) ListPluginsAdmin(w http.ResponseWriter, r *http.Request) {
	items, err := a.DB.ListPlugins(r.Context(), false)
	if err != nil {
		a.failInternal(w, r, "list plugins", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

type pluginReq struct {
	Name    string `json:"name"`
	CSS     string `json:"css"`
	JS      string `json:"js"`
	Enabled *bool  `json:"enabled"`
	Sort    int    `json:"sort"`
}

func (p pluginReq) toModel() repo.Plugin {
	en := p.Enabled == nil || *p.Enabled
	return repo.Plugin{Name: strings.TrimSpace(p.Name), CSS: p.CSS, JS: p.JS, Enabled: en, Sort: p.Sort}
}

func (a *App) CreatePlugin(w http.ResponseWriter, r *http.Request) {
	var req pluginReq
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		a.fail(w, http.StatusBadRequest, "name required")
		return
	}
	id, err := a.DB.CreatePlugin(r.Context(), req.toModel())
	if err != nil {
		if isUnique(err) {
			a.fail(w, http.StatusConflict, "插件名已存在")
			return
		}
		a.failInternal(w, r, "create plugin", err)
		return
	}
	a.publicCache.clear()
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *App) UpdatePlugin(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "bad id")
		return
	}
	var req pluginReq
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		a.fail(w, http.StatusBadRequest, "name required")
		return
	}
	if err := a.DB.UpdatePlugin(r.Context(), id, req.toModel()); err != nil {
		if isUnique(err) {
			a.fail(w, http.StatusConflict, "插件名已存在")
			return
		}
		a.failInternal(w, r, "update plugin", err)
		return
	}
	a.publicCache.clear()
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) DeletePlugin(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := a.DB.DeletePlugin(r.Context(), id); err != nil {
		a.failInternal(w, r, "delete plugin", err)
		return
	}
	a.publicCache.clear()
	w.WriteHeader(http.StatusNoContent)
}
