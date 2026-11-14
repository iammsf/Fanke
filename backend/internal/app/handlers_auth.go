package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"fanke/backend/internal/auth"
	"fanke/backend/internal/repo"
)

const refreshCookie = "fanke_refresh"

type loginReq struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

type tokenOut struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func (a *App) setRefreshCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   !a.Cfg.InsecureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

func (a *App) Login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := decodeJSON(r, &req); err != nil || req.Identifier == "" || req.Password == "" {
		a.fail(w, http.StatusBadRequest, "identifier and password required")
		return
	}
	u, err := a.DB.UserByEmailOrName(r.Context(), req.Identifier)
	if errors.Is(err, repo.ErrNotFound) {
		a.fail(w, http.StatusUnauthorized, "bad credentials")
		return
	}
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	if u.LockedUntil != nil && time.Now().Before(*u.LockedUntil) {
		a.fail(w, http.StatusTooManyRequests, "account locked, try later")
		return
	}
	ok, err := auth.VerifyPassword(req.Password, u.PasswordHash)
	if err != nil || !ok {
		_ = a.DB.RecordLoginFailure(r.Context(), u.ID)
		a.fail(w, http.StatusUnauthorized, "bad credentials")
		return
	}
	_ = a.DB.ClearLoginFailure(r.Context(), u.ID)
	pair, err := a.Signer.Issue(u.ID, u.Role)
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "token error")
		return
	}
	accessTTL, refreshTTL := a.Signer.TTL()
	if err := a.DB.SaveRefreshToken(r.Context(), pair.JTI, u.ID, refreshTTL); err != nil {
		a.fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.setRefreshCookie(w, pair.Refresh, int(refreshTTL/time.Second))
	writeJSON(w, http.StatusOK, tokenOut{AccessToken: pair.Access, ExpiresIn: int(accessTTL / time.Second)})
}

// Refresh 轮换刷新令牌;要求自定义头防跨站携带。
func (a *App) Refresh(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Requested-With") != "fanke" {
		a.fail(w, http.StatusForbidden, "missing X-Requested-With")
		return
	}
	ck, err := r.Cookie(refreshCookie)
	if err != nil {
		a.fail(w, http.StatusUnauthorized, "no refresh token")
		return
	}
	claims, err := a.Signer.Parse(ck.Value)
	if err != nil || claims.Subject != "refresh" || claims.ID == "" {
		a.fail(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	userID, err := a.DB.ConsumeRefreshToken(r.Context(), claims.ID)
	if err != nil {
		a.fail(w, http.StatusUnauthorized, "refresh token revoked or expired")
		return
	}
	u, err := a.DB.UserByID(r.Context(), userID)
	if err != nil {
		a.fail(w, http.StatusUnauthorized, "user gone")
		return
	}
	pair, err := a.Signer.Issue(u.ID, u.Role)
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "token error")
		return
	}
	accessTTL, refreshTTL := a.Signer.TTL()
	if err := a.DB.SaveRefreshToken(r.Context(), pair.JTI, u.ID, refreshTTL); err != nil {
		a.fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.setRefreshCookie(w, pair.Refresh, int(refreshTTL/time.Second))
	writeJSON(w, http.StatusOK, tokenOut{AccessToken: pair.Access, ExpiresIn: int(accessTTL / time.Second)})
}

func (a *App) Logout(w http.ResponseWriter, r *http.Request) {
	if ck, err := r.Cookie(refreshCookie); err == nil {
		if claims, perr := a.Signer.Parse(ck.Value); perr == nil && claims.ID != "" {
			_ = a.DB.RevokeRefreshToken(r.Context(), claims.ID)
		}
	}
	a.setRefreshCookie(w, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) Me(w http.ResponseWriter, r *http.Request) {
	au := currentUser(r)
	u, err := a.DB.UserByID(r.Context(), au.ID)
	if err != nil {
		a.fail(w, http.StatusUnauthorized, "user gone")
		return
	}
	u.PasswordHash = ""
	writeJSON(w, http.StatusOK, u)
}

// ChangePassword 当前登录用户改密码。
func (a *App) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.Password) < 10 {
		a.fail(w, http.StatusBadRequest, "password must be at least 10 chars")
		return
	}
	u, err := a.DB.UserByID(r.Context(), currentUser(r).ID)
	if err != nil {
		a.fail(w, http.StatusUnauthorized, "user gone")
		return
	}
	ok, _ := auth.VerifyPassword(req.Current, u.PasswordHash)
	if !ok {
		a.fail(w, http.StatusUnauthorized, "current password wrong")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "hash failed")
		return
	}
	if err := a.DB.UpdateUserPassword(r.Context(), u.ID, hash); err != nil {
		a.fail(w, http.StatusInternalServerError, "update failed")
		return
	}
	_ = a.DB.RevokeUserSessions(r.Context(), u.ID) // 改密即下线所有设备
	a.pwdCache.Delete(u.ID)                       // 清掉改密时间缓存,立即生效
	w.WriteHeader(http.StatusNoContent)
}

// ipHash 限流键:只信任反向代理写入的 X-Real-IP。
// 不使用 X-Forwarded-For —— 该头可由客户端任意伪造,会让攻击者每请求换个假 IP 绕过限流。
func ipHash(r *http.Request) string {
	ip := strings.TrimSpace(r.Header.Get("X-Real-IP"))
	if ip == "" {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			ip = host
		} else {
			ip = r.RemoteAddr
		}
	}
	sum := sha256.Sum256([]byte(ip + "|fanke-salt"))
	return hex.EncodeToString(sum[:8])
}
