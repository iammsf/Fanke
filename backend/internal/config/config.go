// Package config 集中读取配置:进程环境变量 > 可写配置文件(安装向导生成,0600)。
// 设计目标:项目可复用——全新部署无需手改任何 env 文件,首开网页即走安装向导;
// 数据库断连时也可经向导(凭恢复密钥)找回原数据。
package config

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultPath 运行时可写配置文件(存放数据库连接、JWT 密钥、恢复密钥)。
// 放在独立 state 目录,便于 systemd 沙箱精确放行。
const DefaultPath = "/opt/fanke/state/fanke.env"

func FilePath() string {
	if v := os.Getenv("FANKE_CONFIG"); v != "" {
		return v
	}
	return DefaultPath
}

type Config struct {
	Addr        string // HTTP 监听地址
	DatabaseURL string // postgres://...;空 = 未配置(引导模式)
	JWTSecret   string // 空 = 未生成
	RecoveryKey string // 找回模式口令(向导重配数据库时需出示)
	ConfigPath  string

	AccessTokenTTL  int // 秒
	RefreshTokenTTL int // 秒

	MediaLocalDir string // 本地媒体卷目录
	MediaBaseURL  string // 本地媒体对外前缀
	ThemesDir     string // 运行时上传主题目录
	ExtensionsDir string // 服务端插件目录；默认与 ThemesDir 同级的 extensions
	ImageCacheDir string // 缩略图(WebP)磁盘缓存目录
	// ThumbAllowHosts 允许服务端代为抓取并缩图的外部主机(逗号分隔,防 SSRF)。
	// 用于历史图床(如迁移前的旧图片域名)的存量外链图。
	ThumbAllowHosts string

	RedisURL string

	// UpdateManifestURL 更新清单地址(默认 GitHub 仓库根目录 updates.json;
	// 大陆网络可改为 jsDelivr 等镜像地址,格式一致)。
	UpdateManifestURL string
	// AppDir 安装目录(默认从可执行文件路径推断,如 /opt/fanke)。
	AppDir string
	// WebRoot 前端静态目录(默认按 THEMES_DIR 推断站点 index,再退回 AppDir/web)。
	WebRoot string

	InsecureCookie bool

	BootstrapAdminEmail    string // 可选:自动化部署引导
	BootstrapAdminUsername string
	BootstrapAdminPassword string
}

// Load 宽松加载:缺 DATABASE_URL/JWT_SECRET 不报错,由调用方按引导模式处理。
func Load() *Config {
	file := ReadFile(FilePath())
	get := func(key, def string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		if v := file[key]; v != "" {
			return v
		}
		return def
	}
	return &Config{
		Addr:            get("ADDR", ":8080"),
		DatabaseURL:     get("DATABASE_URL", ""),
		JWTSecret:       get("JWT_SECRET", ""),
		RecoveryKey:     get("RECOVERY_KEY", ""),
		ConfigPath:      FilePath(),
		AccessTokenTTL:  getenvInt("ACCESS_TOKEN_TTL", get, 900),
		RefreshTokenTTL: getenvInt("REFRESH_TOKEN_TTL", get, 30*24*3600),
		MediaLocalDir:   get("MEDIA_LOCAL_DIR", "/data/media"),
		MediaBaseURL:    get("MEDIA_BASE_URL", "/uploads"),
		ThemesDir:       get("THEMES_DIR", "/opt/fanke/themes"),
		ExtensionsDir:   get("EXTENSIONS_DIR", ""),
		// 缩略图缓存默认落在系统临时目录(开发机可用),生产用 THUMB_DIR 指到持久目录;
		// 内容寻址的图片本身不可变,缓存丢了只是重新生成,不会数据不一致。
		ImageCacheDir:          get("THUMB_DIR", filepath.Join(os.TempDir(), "fanke-thumbs")),
		ThumbAllowHosts:        get("THUMB_ALLOW_HOSTS", ""),
		RedisURL:               get("REDIS_URL", ""),
		UpdateManifestURL:      get("UPDATE_MANIFEST_URL", ""),
		AppDir:                 get("APP_DIR", ""),
		WebRoot:                get("WEB_ROOT", ""),
		InsecureCookie:         get("INSECURE_COOKIE", "false") == "true",
		BootstrapAdminEmail:    get("BOOTSTRAP_ADMIN_EMAIL", ""),
		BootstrapAdminUsername: get("BOOTSTRAP_ADMIN_USERNAME", "admin"),
		BootstrapAdminPassword: get("BOOTSTRAP_ADMIN_PASSWORD", ""),
	}
}

// Configured 表示数据库连接信息已持久化(不代表此刻可连通)。
func (c *Config) Configured() bool { return c.DatabaseURL != "" }

// ReadFile 解析 KEY=VALUE 配置(忽略注释/空行)。文件不存在返回空 map。
func ReadFile(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// WriteFile 原子写入配置文件:已存在则保留原权限属主,新建则收紧 0600。
func WriteFile(path string, kv map[string]string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	fmt.Fprintln(w, "# fanke 运行时配置(安装向导生成,勿手动放宽权限)")
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	// 固定顺序,便于 diff
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	for _, k := range keys {
		fmt.Fprintf(w, "%s=%s\n", k, kv[k])
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// BuildDBURL 由向导表单拼 postgres:// URL(密码做百分号转义)。
func BuildDBURL(host, port, dbname, user, password, sslmode string) (string, error) {
	if host == "" || dbname == "" || user == "" {
		return "", fmt.Errorf("host/dbname/user 不能为空")
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return "", fmt.Errorf("端口无效")
	}
	if sslmode == "" {
		sslmode = "disable"
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		user, urlEscape(password), host, port, dbname, sslmode), nil
}

func urlEscape(s string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '!' || c == '-' || c == '.' || c == '_' || c == '~' ||
			(c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&0x0f])
		}
	}
	return b.String()
}

// RandomHex 生成 n 字节随机 hex(用于 JWT_SECRET / RECOVERY_KEY)。
func RandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func getenvInt(key string, get func(string, string) string, def int) int {
	v := strings.TrimSpace(get(key, ""))
	if v == "" {
		return def
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	return def
}
