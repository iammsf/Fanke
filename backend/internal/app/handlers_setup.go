package app

import (
	"context"
	"crypto/subtle"
	"log"
	"net/http"
	"strings"
	"time"

	"fanke/backend/internal/auth"
	"fanke/backend/internal/config"
	"fanke/backend/internal/repo"
	"fanke/backend/internal/secret"
)

// setupState: ok=数据库正常 | unconfigured=从未配置 | broken=已配置但连不上(找回模式)。
func (a *App) setupState() string {
	if !a.Cfg.Configured() {
		return "unconfigured"
	}
	if a.DB == nil {
		return "broken"
	}
	return "ok"
}

// SetupStatus 向导状态查询(引导模式下唯一可用的业务探测口)。
func (a *App) SetupStatus(w http.ResponseWriter, r *http.Request) {
	state := a.setupState()
	out := map[string]any{"state": state, "configured": a.Cfg.Configured(), "needs_setup": false}
	if state == "ok" {
		n, err := a.DB.CountUsers(r.Context())
		if err == nil {
			out["needs_setup"] = n == 0
			out["has_users"] = n > 0
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type setupDBReq struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	DBName   string `json:"dbname"`
	User     string `json:"user"`
	Password string `json:"password"`
	SSLMode  string `json:"sslmode"`
	// 找回模式(已配置但断库)必须出示配置文件里的 RECOVERY_KEY,防止外人把站点指向其数据库。
	RecoveryKey string `json:"recovery_key"`
}

// SetupDB 向导第一步:测试数据库连接。成功后写入 0600 配置文件并热接入。
func (a *App) SetupDB(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.setupState()
	if state == "ok" {
		a.fail(w, http.StatusForbidden, "系统已配置且数据库正常,向导已锁定")
		return
	}
	var req setupDBReq
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Port == "" {
		req.Port = "5432"
	}
	if state == "broken" {
		if a.Cfg.RecoveryKey == "" {
			a.fail(w, http.StatusForbidden, "配置文件缺少 RECOVERY_KEY,无法在线找回,请在服务器上补写后重试")
			return
		}
		if subtle.ConstantTimeCompare([]byte(req.RecoveryKey), []byte(a.Cfg.RecoveryKey)) != 1 {
			a.fail(w, http.StatusUnauthorized, "恢复密钥不正确(见服务器 "+a.Cfg.ConfigPath+" 中 RECOVERY_KEY)")
			return
		}
	}
	url, err := config.BuildDBURL(req.Host, req.Port, req.DBName, req.User, req.Password, req.SSLMode)
	if err != nil {
		a.fail(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	db, err := repo.Open(ctx, url)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "数据库连接失败,请检查主机/端口/库名/账号密码")
		return
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		a.fail(w, http.StatusBadRequest, "数据表初始化失败: "+err.Error())
		return
	}
	if err := a.persistConfig(req); err != nil {
		db.Close()
		a.fail(w, http.StatusInternalServerError, "配置文件写入失败: "+err.Error())
		return
	}
	// 敏感字段加密:主密钥来自配置文件中的 RECOVERY_KEY
	if box, berr := secret.NewBox(a.Cfg.RecoveryKey); berr == nil && box != nil {
		db.Box = box
		if err := db.EncryptLegacySecrets(ctx); err != nil {
			log.Printf("历史明文密钥迁移失败: %v", err)
		}
	}
	a.attachDB(db, a.Cfg.JWTSecret)
	n, _ := db.CountUsers(ctx)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "needs_setup": n == 0, "has_users": n > 0})
}

// persistConfig 合并写回配置文件(0600):保留既有密钥,缺则生成。
func (a *App) persistConfig(req setupDBReq) error {
	kv := config.ReadFile(a.Cfg.ConfigPath)
	url, err := config.BuildDBURL(req.Host, req.Port, req.DBName, req.User, req.Password, req.SSLMode)
	if err != nil {
		return err
	}
	kv["DATABASE_URL"] = url
	if len(kv["JWT_SECRET"]) < 32 {
		kv["JWT_SECRET"] = config.RandomHex(32)
	}
	if len(kv["RECOVERY_KEY"]) < 16 {
		kv["RECOVERY_KEY"] = config.RandomHex(16)
	}
	setDefault := func(k, v string) {
		if kv[k] == "" {
			kv[k] = v
		}
	}
	setDefault("ADDR", a.Cfg.Addr)
	setDefault("MEDIA_LOCAL_DIR", a.Cfg.MediaLocalDir)
	setDefault("MEDIA_BASE_URL", a.Cfg.MediaBaseURL)
	setDefault("THEMES_DIR", a.Cfg.ThemesDir)
	if err := config.WriteFile(a.Cfg.ConfigPath, kv); err != nil {
		return err
	}
	a.Cfg.DatabaseURL = url
	a.Cfg.JWTSecret = kv["JWT_SECRET"]
	a.Cfg.RecoveryKey = kv["RECOVERY_KEY"]
	return nil
}

type setupReq struct {
	Site struct {
		Title    string `json:"title"`
		Subtitle string `json:"subtitle"`
	} `json:"site"`
	Admin struct {
		Username    string `json:"username"`
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	} `json:"admin"`
}

// SetupComplete 向导第二步(仅空库):创建首个管理员并写入默认设置,直接发令牌。
// 库中已有用户时不可用——找回数据只需第一步连上原库即可,原账号密码不变。
func (a *App) SetupComplete(w http.ResponseWriter, r *http.Request) {
	if a.setupState() != "ok" {
		a.fail(w, http.StatusServiceUnavailable, "请先完成数据库配置")
		return
	}
	n, err := a.DB.CountUsers(r.Context())
	if err != nil {
		a.failInternal(w, r, "setup count", err)
		return
	}
	if n > 0 {
		a.fail(w, http.StatusForbidden, "已安装,安装入口已关闭")
		return
	}
	var req setupReq
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.Admin.Username = strings.TrimSpace(req.Admin.Username)
	req.Admin.Email = strings.TrimSpace(req.Admin.Email)
	if !usernameRe.MatchString(req.Admin.Username) || !strings.Contains(req.Admin.Email, "@") || len(req.Admin.Password) < 10 {
		a.fail(w, http.StatusBadRequest, "用户名/邮箱/密码(≥10位)不符合要求")
		return
	}
	if strings.TrimSpace(req.Site.Title) == "" {
		req.Site.Title = "我的博客"
	}
	name := strings.TrimSpace(req.Admin.DisplayName)
	if name == "" {
		name = req.Admin.Username
	}
	hash, err := auth.HashPassword(req.Admin.Password)
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "hash failed")
		return
	}
	id, err := a.DB.CreateUser(r.Context(), req.Admin.Email, req.Admin.Username, hash, "admin", name)
	if err != nil {
		if isUnique(err) {
			a.fail(w, http.StatusConflict, "邮箱或用户名已存在")
			return
		}
		a.failInternal(w, r, "setup create admin", err)
		return
	}
	defer a.publicCache.clear()
	// 默认设置(幂等:仅缺失时写入)
	defaults := map[string]any{
		"site":           map[string]string{"title": req.Site.Title, "subtitle": req.Site.Subtitle, "author": name},
		"theme":          map[string]string{"name": "element-plus"},
		"plugins":        map[string]bool{"shiki": true, "heading-anchors": true, "lazy-images": true},
		"comment_policy": map[string]string{"mode": "moderate"},
		"nav":            []map[string]string{{"label": "首页", "href": "/"}, {"label": "分类", "href": "/categories"}, {"label": "标签", "href": "/tags"}, {"label": "搜索", "href": "/search"}},
	}
	for k, v := range defaults {
		if _, err := a.DB.GetSetting(r.Context(), k); err != nil {
			_ = a.DB.PutSetting(r.Context(), k, v)
		}
	}

	pair, err := a.Signer.Issue(id, "admin")
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "token error")
		return
	}
	_, refreshTTL := a.Signer.TTL()
	_ = a.DB.SaveRefreshToken(r.Context(), pair.JTI, id, refreshTTL)
	a.setRefreshCookie(w, pair.Refresh, int(refreshTTL/time.Second))
	writeJSON(w, http.StatusCreated, map[string]any{"access_token": pair.Access, "user_id": id})
}
