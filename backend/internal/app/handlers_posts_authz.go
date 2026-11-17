package app

import (
	"errors"
	"net/http"

	"fanke/backend/internal/repo"
)

// canEditPost 归属判定:admin 管理所有文章,author 只能动自己的。
func canEditPost(u authedUser, authorID int64) bool {
	return u.Role == "admin" || authorID == u.ID
}

// requireOwnPost 按 id 取文章并做归属校验,通过才返回文章。
func (a *App) requireOwnPost(w http.ResponseWriter, r *http.Request, id int64) (*repo.Post, bool) {
	p, err := a.DB.PostByID(r.Context(), id)
	if errors.Is(err, repo.ErrNotFound) {
		a.fail(w, http.StatusNotFound, "not found")
		return nil, false
	}
	if err != nil {
		a.failInternal(w, r, "load post", err)
		return nil, false
	}
	if !canEditPost(currentUser(r), p.AuthorID) {
		a.fail(w, http.StatusForbidden, "只能操作自己的文章")
		return nil, false
	}
	return p, true
}
