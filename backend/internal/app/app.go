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

	// S3 配置快照缓存:thumbSource 与 mediaStorage 是每次图片请求都要走的热路径,
	// 而 repo.S3Settings 每次都要读库并对每个桶做一次 AES-GCM 解密。
	s3SettMu      sync.Mutex
	s3SettGen     uint64
	s3SettExpires time.Time
	s3Sett        repo.S3Settings

	// 外链图代抓客户端(重定向也过白名单,防 SSRF)
	thumbOnce sync.Once
	thumbHTTP *http.Client

	// pwd_changed_at 短缓存:避免每个鉴权请求都查一次库
	pwdCache sync.Map

	// 全局更新互斥锁:防止本体/插件/主题更新并发写盘破坏目录状态
	updateMu sync.Mutex
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
		AI:     ai.NewClientWithAllowlist(db, cfg.AllowPrivateAIHosts),
	}
	ConfigureUpdateProxy(cfg.UpdateProxy)
	ConfigureUpdateHTTP(cfg.AllowInsecureUpdateHTTP)
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
	a.AI = ai.NewClientWithAllowlist(db, a.Cfg.AllowPrivateAIHosts)
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

// s3Settings 是 repo.S3Settings 的缓存版,只给每次图片请求都要走的热路径用
// (thumbSource 判定图源、mediaStorage 取存储驱动)。失效挂在 publicCache 的换代号上:
// 改 S3 配置的入口会 publicCache.clear(),保存后立即生效,不需要单独的失效路径。
// 读写 S3 设置的管理接口仍直接调 a.DB.S3Settings,免得看到自己刚写下去的旧值。
func (a *App) s3Settings(ctx context.Context) (repo.S3Settings, error) {
	if a.DB == nil {
		return repo.S3Settings{}, nil
	}
	gen := a.publicCache.gen()
	a.s3SettMu.Lock()
	if a.s3SettGen == gen && time.Now().Before(a.s3SettExpires) {
		s := a.s3Sett
		a.s3SettMu.Unlock()
		// 桶切片复制一份再交出去:缓存是所有调用方共享的,不能让别人改到快照里的凭据。
		s.Buckets = append([]media.S3Config(nil), s.Buckets...)
		return s, nil
	}
	a.s3SettMu.Unlock()

	s, err := a.DB.S3Settings(ctx)
	if err != nil {
		return repo.S3Settings{}, err // 失败不缓存,下一次请求重试
	}

	a.s3SettMu.Lock()
	// 换代号变了说明期间有人清过缓存,不能把旧快照回填进新一代
	if a.publicCache.gen() == gen {
		a.s3SettGen, a.s3Sett = gen, s
		a.s3Sett.Buckets = append([]media.S3Config(nil), s.Buckets...)
		a.s3SettExpires = time.Now().Add(publicCacheTTL)
	}
	a.s3SettMu.Unlock()

	s.Buckets = append([]media.S3Config(nil), s.Buckets...)
	return s, nil
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
