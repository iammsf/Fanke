package app

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func (a *App) ListTerms(w http.ResponseWriter, r *http.Request) {
	typ := r.URL.Query().Get("type")
	if typ != "" && typ != "category" && typ != "tag" {
		a.fail(w, http.StatusBadRequest, "type must be category or tag")
		return
	}
	items, err := a.DB.ListTerms(r.Context(), typ)
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSONCached(w, r, http.StatusOK, items)
}

type termReq struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
	Type string `json:"type"`
}

func (a *App) CreateTerm(w http.ResponseWriter, r *http.Request) {
	var req termReq
	if err := decodeJSON(r, &req); err != nil || req.Name == "" || (req.Type != "category" && req.Type != "tag") {
		a.fail(w, http.StatusBadRequest, "name and type required")
		return
	}
	if req.Slug == "" {
		req.Slug = req.Name
	}
	id, err := a.DB.CreateTerm(r.Context(), req.Name, req.Slug, req.Type)
	if err != nil {
		if isUnique(err) {
			a.fail(w, http.StatusConflict, "slug exists")
			return
		}
		a.fail(w, http.StatusInternalServerError, "create failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *App) UpdateTerm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "bad id")
		return
	}
	var req termReq
	if err := decodeJSON(r, &req); err != nil || req.Name == "" || req.Slug == "" {
		a.fail(w, http.StatusBadRequest, "name and slug required")
		return
	}
	if err := a.DB.UpdateTerm(r.Context(), id, req.Name, req.Slug); err != nil {
		if isUnique(err) {
			a.fail(w, http.StatusConflict, "slug exists")
			return
		}
		a.fail(w, http.StatusInternalServerError, "update failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) DeleteTerm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := a.DB.DeleteTerm(r.Context(), id); err != nil {
		a.fail(w, http.StatusInternalServerError, "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
