package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type ctxKey int

const userKey ctxKey = 1

type authedUser struct {
	ID   int64
	Role string
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// requireDB 数据库未就绪(未配置/断连)时业务接口统一 503,前端据此跳 /setup。
func (a *App) requireDB(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.DB == nil {
			a.fail(w, http.StatusServiceUnavailable, "not_configured")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// bearerAuth 校验 Authorization: Bearer <access token>,注入用户上下文。
func (a *App) bearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			a.fail(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := a.Signer.Parse(strings.TrimPrefix(h, "Bearer "))
		if err != nil || claims.Subject != "access" {
			a.fail(w, http.StatusUnauthorized, "invalid token")
			return
		}
		// 查库校验用户仍存在、获取实时角色并核对改密时间。
		// 发生错误(如用户已删除返回 ErrNoRows)必须 fail-closed 返回 401,绝不放行。
		role, changedAt, err := a.userAuthState(r.Context(), claims.UserID)
		if err != nil {
			a.fail(w, http.StatusUnauthorized, "invalid user session")
			return
		}
		// 改密后旧 access token 立即失效(签发时间早于密码修改时间)
		if changedAt != nil && (claims.IssuedAt == nil || claims.IssuedAt.Time.Before(*changedAt)) {
			a.fail(w, http.StatusUnauthorized, "token revoked by password change")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, authedUser{ID: claims.UserID, Role: role})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// optionalBearer 有 Bearer token 就解析并注入用户,没有或无效也不报错。
// 仅用于公开读接口:未公开文章(草稿)只对已登录的后台用户显示。
func (a *App) optionalBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if strings.HasPrefix(h, "Bearer ") {
			if claims, err := a.Signer.Parse(strings.TrimPrefix(h, "Bearer ")); err == nil && claims.Subject == "access" {
				// 与 bearerAuth 一致:用户被删或改密/降权后的旧 token 实时反映数据库状态。
				role, changedAt, err := a.userAuthState(r.Context(), claims.UserID)
				revoked := err != nil || (changedAt != nil && (claims.IssuedAt == nil || claims.IssuedAt.Time.Before(*changedAt)))
				if !revoked {
					r = r.WithContext(context.WithValue(r.Context(), userKey, authedUser{ID: claims.UserID, Role: role}))
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) requireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return a.bearerAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := currentUser(r)
			switch role {
			case "admin":
				if u.Role != "admin" {
					a.fail(w, http.StatusForbidden, "admin only")
					return
				}
			case "author":
				if u.Role != "admin" && u.Role != "author" {
					a.fail(w, http.StatusForbidden, "forbidden")
					return
				}
			}
			next.ServeHTTP(w, r)
		}))
	}
}

func currentUser(r *http.Request) authedUser {
	v, _ := r.Context().Value(userKey).(authedUser)
	return v
}

// userAuthState 读取用户当前角色与改密时间,带 30 秒内存缓存。
// 否则每个鉴权请求都要多打一次数据库(公开接口除外,但管理端轮询很频繁)。
func (a *App) userAuthState(ctx context.Context, uid int64) (string, *time.Time, error) {
	if v, ok := a.pwdCache.Load(uid); ok {
		if s, ok := v.(pwdStamp); ok && time.Since(s.at) < 30*time.Second {
			return s.role, s.changed, nil
		}
	}
	var role string
	var changed *time.Time
	if err := a.DB.QueryRow(ctx, `SELECT role, pwd_changed_at FROM users WHERE id=$1`, uid).Scan(&role, &changed); err != nil {
		return "", nil, err
	}
	a.pwdCache.Store(uid, pwdStamp{at: time.Now(), role: role, changed: changed})
	return role, changed, nil
}

type pwdStamp struct {
	at      time.Time
	role    string
	changed *time.Time
}
