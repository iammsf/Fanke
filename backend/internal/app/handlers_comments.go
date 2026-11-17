package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"fanke/backend/internal/render"
	"fanke/backend/internal/repo"
)

type commentReq struct {
	AuthorName  string `json:"author_name"`
	AuthorEmail string `json:"author_email"`
	Content     string `json:"content"`
	ParentID    *int64 `json:"parent_id"`
	Website     string `json:"website"` // 蜜罐,必须为空
}

func (a *App) ListComments(w http.ResponseWriter, r *http.Request) {
	slug := routeSlug(r)
	p, err := a.DB.CommentPostBySlug(r.Context(), slug)
	if errors.Is(err, repo.ErrNotFound) {
		a.fail(w, http.StatusNotFound, "post not found")
		return
	}
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !visiblePost(r, p) {
		a.fail(w, http.StatusNotFound, "post not found")
		return
	}
	items, err := a.DB.ListComments(r.Context(), p.ID, []string{"approved"})
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "query failed")
		return
	}
	out := map[string]any{"items": items, "allow_comments": p.AllowComments}
	if p.IsPublic {
		writeJSONCached(w, r, http.StatusOK, out)
		return
	}
	writeJSON(w, http.StatusOK, out) // 草稿预览不可进公共缓存
}

func (a *App) PostComment(w http.ResponseWriter, r *http.Request) {
	slug := routeSlug(r)
	p, err := a.DB.PostBySlug(r.Context(), slug, true, false)
	if errors.Is(err, repo.ErrNotFound) {
		a.fail(w, http.StatusNotFound, "post not found")
		return
	}
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !visiblePost(r, p) {
		a.fail(w, http.StatusNotFound, "post not found")
		return
	}
	if !p.AllowComments {
		a.fail(w, http.StatusForbidden, "comments closed")
		return
	}
	if a.commentMode() == "close" {
		a.fail(w, http.StatusForbidden, "comments globally closed")
		return
	}
	if !a.allowComment(r) {
		a.fail(w, http.StatusTooManyRequests, "too many requests")
		return
	}
	var req commentReq
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Website != "" {
		// 蜜罐命中:静默丢弃
		w.WriteHeader(http.StatusNoContent)
		return
	}
	req.AuthorName = strings.TrimSpace(req.AuthorName)
	req.Content = strings.TrimSpace(req.Content)
	if req.AuthorName == "" || len(req.AuthorName) > 60 || len(req.Content) < 2 || len(req.Content) > 4000 {
		a.fail(w, http.StatusBadRequest, "name(1-60) and content(2-4000) required")
		return
	}
	c := repo.Comment{
		PostID: p.ID, ParentID: req.ParentID,
		AuthorName: req.AuthorName, AuthorEmail: strings.TrimSpace(req.AuthorEmail),
		Content:    render.SanitizeHTML(toHTML(req.Content)),
		Status:     a.commentStatus(),
		IPHash:     a.ipHash(r),
	}
	if req.ParentID != nil {
		ok, err := a.DB.CommentParentExists(r.Context(), p.ID, *req.ParentID)
		if err != nil {
			a.fail(w, http.StatusInternalServerError, "query failed")
			return
		}
		if !ok {
			a.fail(w, http.StatusBadRequest, "parent not found")
			return
		}
	}
	id, err := a.DB.CreateComment(r.Context(), c)
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "save failed")
		return
	}
	code := http.StatusCreated
	if c.Status == "pending" {
		code = http.StatusAccepted
	}
	writeJSON(w, code, map[string]any{"id": id, "status": c.Status})
}

// toHTML 把纯文本评论转成安全段落(换行→p)。
func toHTML(text string) string {
	parts := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n")
	var b strings.Builder
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			b.WriteString("<p>")
			b.WriteString(strings.ReplaceAll(p, "\n", "<br>"))
			b.WriteString("</p>")
		}
	}
	return b.String()
}

func (a *App) commentStatus() string {
	if a.commentMode() == "open" {
		return "approved"
	}
	return "pending"
}

// commentMode 读取 comment_policy.mode(moderate/open/close),默认 moderate。
func (a *App) commentMode() string {
	raw, err := a.DB.GetSetting(context.Background(), "comment_policy")
	if err == nil {
		var pol struct {
			Mode string `json:"mode"`
		}
		if json.Unmarshal(raw, &pol) == nil && pol.Mode != "" {
			return pol.Mode
		}
	}
	return "moderate"
}

func (a *App) ListCommentsAdmin(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	limit, _ := strconv.Atoi(r.URL.Query().Get("per"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	items, total, err := a.DB.ListAllComments(r.Context(), status, limit, (page-1)*limit)
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (a *App) SetCommentStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "bad id")
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(r, &body); err != nil ||
		(body.Status != "approved" && body.Status != "pending" && body.Status != "spam") {
		a.fail(w, http.StatusBadRequest, "status must be approved/pending/spam")
		return
	}
	if err := a.DB.SetCommentStatus(r.Context(), id, body.Status); err != nil {
		a.fail(w, http.StatusInternalServerError, "update failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) DeleteComment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := a.DB.DeleteComment(r.Context(), id); err != nil {
		a.fail(w, http.StatusInternalServerError, "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
