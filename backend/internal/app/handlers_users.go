package app

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"fanke/backend/internal/auth"
	"fanke/backend/internal/repo"
)

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_\-]{2,40}$`)

func (a *App) ListUsersAdmin(w http.ResponseWriter, r *http.Request) {
	users, err := a.DB.ListUsers(r.Context())
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "query failed")
		return
	}
	for i := range users {
		users[i].PasswordHash = ""
	}
	writeJSON(w, http.StatusOK, users)
}

type userReq struct {
	Email       string  `json:"email"`
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	Role        string  `json:"role"`
	AvatarURL   string  `json:"avatar_url"`
	Bio         *string `json:"bio"`
	Password    string  `json:"password"` // 仅创建时使用
}

// CreateUserAdmin 管理员新建用户。
func (a *App) CreateUserAdmin(w http.ResponseWriter, r *http.Request) {
	var req userReq
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	req.Username = strings.TrimSpace(req.Username)
	if req.Email == "" || !usernameRe.MatchString(req.Username) || len(req.Password) < 10 {
		a.fail(w, http.StatusBadRequest, "邮箱/ID(2-40 字母数字_-)/密码(≥10 位) 不合法")
		return
	}
	if req.Role != "admin" && req.Role != "author" {
		req.Role = "author"
	}
	name := req.DisplayName
	if name == "" {
		name = req.Username
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "hash failed")
		return
	}
	id, err := a.DB.CreateUser(r.Context(), req.Email, req.Username, hash, req.Role, name)
	if err != nil {
		if isUnique(err) {
			a.fail(w, http.StatusConflict, "邮箱或用户名已存在")
			return
		}
		a.failInternal(w, r, "create user", err)
		return
	}
	a.publicCache.clear()
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// UpdateUserAdmin 管理员修改任意用户资料(不能改自己的角色,防止自降权锁死)。
func (a *App) UpdateUserAdmin(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "bad id")
		return
	}
	var req userReq
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Username != "" && !usernameRe.MatchString(req.Username) {
		a.fail(w, http.StatusBadRequest, "用户名格式不合法")
		return
	}
	if req.Role != "" && req.Role != "admin" && req.Role != "author" {
		a.fail(w, http.StatusBadRequest, "role 仅 admin/author")
		return
	}
	au := currentUser(r)
	if id == au.ID && req.Role != "" && req.Role != au.Role {
		a.fail(w, http.StatusBadRequest, "不能修改自己的角色")
		return
	}
	if err := a.DB.UpdateUser(r.Context(), id, strings.TrimSpace(req.Email), strings.TrimSpace(req.Username),
		strings.TrimSpace(req.DisplayName), req.Role, req.AvatarURL, req.Bio); err != nil {
		if isUnique(err) {
			a.fail(w, http.StatusConflict, "邮箱或用户名已存在")
			return
		}
		a.failInternal(w, r, "update user", err)
		return
	}
	a.publicCache.clear()
	w.WriteHeader(http.StatusNoContent)
}

// ResetUserPassword 管理员重置他人密码(不要求旧密码)。
func (a *App) ResetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.Password) < 10 {
		a.fail(w, http.StatusBadRequest, "密码至少 10 位")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "hash failed")
		return
	}
	if err := a.DB.UpdateUserPassword(r.Context(), id, hash); err != nil {
		a.failInternal(w, r, "reset password", err)
		return
	}
	_ = a.DB.RevokeUserSessions(r.Context(), id) // 管理员重置同样使该用户所有会话失效
	a.pwdCache.Delete(id)                       // 清掉改密时间缓存,立即生效
	w.WriteHeader(http.StatusNoContent)
}

// DeleteUserAdmin 删除用户(不能删自己;文章作者保留为占位需要外键处理,这里禁止删仍有文章的用户)。
func (a *App) DeleteUserAdmin(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if id == currentUser(r).ID {
		a.fail(w, http.StatusBadRequest, "不能删除自己")
		return
	}
	var posts int
	if err := a.DB.QueryRow(r.Context(), `SELECT count(*) FROM posts WHERE author_id=$1`, id).Scan(&posts); err == nil && posts > 0 {
		a.fail(w, http.StatusConflict, "该用户仍有文章,请先转移作者")
		return
	}
	if err := a.DB.DeleteUser(r.Context(), id); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			a.fail(w, http.StatusNotFound, "用户不存在")
			return
		}
		a.failInternal(w, r, "delete user", err)
		return
	}
	a.publicCache.clear()
	w.WriteHeader(http.StatusNoContent)
}

// UpdateMyProfile 任何登录用户改自己的昵称/邮箱/头像/简介。
func (a *App) UpdateMyProfile(w http.ResponseWriter, r *http.Request) {
	var req userReq
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, http.StatusBadRequest, "invalid json")
		return
	}
	au := currentUser(r)
	if err := a.DB.UpdateUser(r.Context(), au.ID, strings.TrimSpace(req.Email), "", strings.TrimSpace(req.DisplayName),
		"", req.AvatarURL, req.Bio); err != nil {
		if isUnique(err) {
			a.fail(w, http.StatusConflict, "邮箱已存在")
			return
		}
		a.failInternal(w, r, "update profile", err)
		return
	}
	a.publicCache.clear()
	w.WriteHeader(http.StatusNoContent)
}
