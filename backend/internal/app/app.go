// Package app 组装依赖并暴露 HTTP 处理器。
package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"fanke/backend/internal/ai"
	"fanke/backend/internal/auth"
	"fanke/backend/internal/config"
	"fanke/backend/internal/media"
	"fanke/backend/internal/repo"
)

type App struct {
	Cfg    *config.Config
	DB     *repo.DB // nil = 引导模式(未配置或数据库断连),仅 setup/healthz 可用
	Signer *auth.Signer
	Limits *AppLimits
	Local  *media.Local
	Thumbs *media.Thumbnailer // 按需缩图(WebP)服务
	AI     *ai.Client         // AI 连接管理客户端

	// 引导模式下由 setup 向导热接入数据库时交换 DB/Signer
	mu sync.Mutex

	publicCache publicCache

	// S3 驱动按 settings.s3_config 动态构建并缓存
	s3Mu     sync.Mutex
	s3Hash   string
	s3Cached media.Storage
	s3Err    error
	s3Stores map[string]media.Storage
	s3Errors map[string]error

	// 外链图代抓客户端(重定向也过白名单,防 SSRF)
	thumbOnce sync.Once
	thumbHTTP *http.Client

	// pwd_changed_at 短缓存:避免每个鉴权请求都查一次库
	pwdCache sync.Map
}

func New(cfg *config.Config, db *repo.DB) (*App, error) {
	local, err := media.NewLocal(cfg.MediaLocalDir, cfg.MediaBaseURL)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.ThemesDir, 0o755); err != nil {
		return nil, err
	}
	if cfg.ExtensionsDir == "" {
		cfg.ExtensionsDir = filepath.Join(filepath.Dir(filepath.Clean(cfg.ThemesDir)), "extensions")
	}
	if err := os.MkdirAll(cfg.ExtensionsDir, 0o755); err != nil {
		return nil, err
	}
	media.ConfigurePluginRoot(cfg.ExtensionsDir)
	thumbs, err := media.NewThumbnailer(cfg.ImageCacheDir)
	if err != nil {
		return nil, err
	}
	a := &App{
		Cfg:    cfg,
		DB:     db,
		Limits: NewLimits(),
		Local:  local,
		Thumbs: thumbs,
		AI:     ai.NewClient(db),
	}
	ConfigureUpdateProxy(cfg.UpdateProxy)
	// Signer 必须始终可用:否则配置了 DATABASE_URL 却漏配 JWT_SECRET 时,
	// 登录/鉴权接口会因 nil 指针 panic。缺密钥时生成进程内临时密钥并告警。
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = config.RandomHex(32)
		log.Printf("JWT_SECRET 未配置,已生成临时密钥;重启后所有登录态失效,请在 %s 中持久化", cfg.ConfigPath)
	}
	a.Signer = a.newSigner(cfg.JWTSecret)
	return a, nil
}

func (a *App) newSigner(secret string) *auth.Signer {
	return auth.NewSigner(secret,
		time.Duration(a.Cfg.AccessTokenTTL)*time.Second,
		time.Duration(a.Cfg.RefreshTokenTTL)*time.Second)
}

// attachDB 向导配置数据库成功后热接入(同时确保 JWT 密钥已生成)。
// 调用方必须已持有 a.mu。
func (a *App) attachDB(db *repo.DB, secret string) {
	a.DB = db
	// AI 客户端持有启动时的 db;引导模式下那是 nil,不重建会让 ai.chat 一直 panic 到重启。
	a.AI = ai.NewClient(db)
	a.Cfg.JWTSecret = secret
	a.Signer = a.newSigner(secret)
	a.s3Mu.Lock()
	a.s3Hash, a.s3Cached, a.s3Err = "", nil, nil // 换库后重置 S3 缓存
	a.s3Stores, a.s3Errors = nil, nil
	a.s3Mu.Unlock()
	a.publicCache.clear()
}

// s3Store 不受新上传开关影响，保证历史媒体仍可读取和删除。
func (a *App) s3Store(ctx context.Context) (media.Storage, media.S3Config, error) {
	cfg, err := a.DB.S3Config(ctx)
	if err != nil {
		return nil, cfg, err
	}
	return a.s3StoreConfig(ctx, cfg)
}

func (a *App) s3StoreConfig(ctx context.Context, cfg media.S3Config) (media.Storage, media.S3Config, error) {
	raw, _ := json.Marshal(cfg)
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	a.s3Mu.Lock()
	defer a.s3Mu.Unlock()
	if a.s3Stores == nil {
		a.s3Stores, a.s3Errors = map[string]media.Storage{}, map[string]error{}
	}
	if store, ok := a.s3Stores[hash]; ok {
		return store, cfg, a.s3Errors[hash]
	}
	if err, ok := a.s3Errors[hash]; ok {
		return nil, cfg, err
	}
	store, err := media.NewS3FromConfig(ctx, cfg)
	a.s3Stores[hash], a.s3Errors[hash] = store, err
	if err != nil {
		return nil, cfg, err
	}
	return store, cfg, nil
}

type errResp struct {
	Error string `json:"error"`
}

func (a *App) fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, errResp{Error: msg})
}

// failInternal 记录真实错误并返回脱敏信息。
func (a *App) failInternal(w http.ResponseWriter, r *http.Request, op string, err error) {
	log.Printf("internal error op=%s: %v", op, err)
	a.fail(w, http.StatusInternalServerError, op+" failed")
}
