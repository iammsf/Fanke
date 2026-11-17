package app

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"golang.org/x/time/rate"
)

// limiterSet 按 (kind, ipHash) 维护令牌桶,空闲桶定期清理。
type limiterSet struct {
	mu       sync.Mutex
	kind     string
	rate     rate.Limit
	burst    int
	limiters map[string]*entry
	lastGC   time.Time
}

type entry struct {
	l  *rate.Limiter
	ts time.Time
}

func newLimiterSet(kind string, r rate.Limit, burst int) *limiterSet {
	return &limiterSet{kind: kind, rate: r, burst: burst, limiters: map[string]*entry{}}
}

func (s *limiterSet) allow(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if now.Sub(s.lastGC) > 10*time.Minute {
		for k, e := range s.limiters {
			if now.Sub(e.ts) > 30*time.Minute {
				delete(s.limiters, k)
			}
		}
		s.lastGC = now
	}
	e, ok := s.limiters[key]
	if !ok {
		e = &entry{l: rate.NewLimiter(s.rate, s.burst)}
		s.limiters[key] = e
	}
	e.ts = now
	return e.l.AllowN(now, 1)
}

type AppLimits struct {
	Login   *limiterSet
	Comment *limiterSet
	// Writes 同一用户对同一写接口的并发去重(前端已有全屏遮罩,后端再兜一层)
	Writes *inflightSet
	// Hook 按「插件 + hook」限制调用频率,防止插件失控刷接口/刷 AI 额度
	Hook *hookLimiter
}

func NewLimits() *AppLimits {
	return &AppLimits{
		Login:   newLimiterSet("login", rate.Every(15*time.Second), 5), // 每 IP 5 次/分钟
		Comment: newLimiterSet("comment", rate.Every(20*time.Second), 3),
		Writes:  newInflightSet(),
		Hook:    newHookLimiter(),
	}
}

// hookPerMinute 返回某个 hook 的每分钟调用上限(未单独配置则用默认值)。
func hookPerMinute(hook string) int {
	if n := hookRateLimits[hook]; n > 0 {
		return n
	}
	return defaultHookPerMinute
}

const defaultHookPerMinute = 60

// hookRateLimits 各 hook 的每分钟上限(按插件计,一个插件一个桶)。
var hookRateLimits = map[string]int{
	"store.ensure_table": 5,
	"store.save":         120,
	"store.get":          300,
	"store.get_batch":    120,
	"store.list":         60,
	"store.import":       12,
	"store.export":       20,
	"store.dedup":        10,
	"store.delete":       60,
	"store.stats":        120,
	"media.list":         60,
	"http.check_url":     30,
	"ai.chat":            20,
	"notify.send":        60,
}

// hookLimiter 按 (slug, hook) 维护令牌桶:每分钟最多 N 次,桶容量即 N(允许一分钟内突发)。
type hookLimiter struct {
	mu      sync.Mutex
	entries map[string]*hookEntry
	lastGC  time.Time
}

type hookEntry struct {
	l  *rate.Limiter
	ts time.Time
}

func newHookLimiter() *hookLimiter {
	return &hookLimiter{entries: map[string]*hookEntry{}}
}

func (h *hookLimiter) allow(slug, hook string) bool {
	limit := hookPerMinute(hook)
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if now.Sub(h.lastGC) > 10*time.Minute {
		for k, e := range h.entries {
			if now.Sub(e.ts) > 30*time.Minute {
				delete(h.entries, k)
			}
		}
		h.lastGC = now
	}
	key := slug + "|" + hook
	e, ok := h.entries[key]
	if !ok {
		e = &hookEntry{l: rate.NewLimiter(rate.Every(time.Minute/time.Duration(limit)), limit)}
		h.entries[key] = e
	}
	e.ts = now
	return e.l.AllowN(now, 1)
}

func (a *App) allowLogin(r *http.Request) bool {
	return a.Limits.Login.allow(a.ipHash(r))
}

func (a *App) allowComment(r *http.Request) bool {
	return a.Limits.Comment.allow(a.ipHash(r))
}

// noStore API 响应一律禁止缓存。站点若挂在 CloudFront 等 CDN 上,
// 避免保存文章后列表/详情仍读到边缘节点里的旧 JSON。
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		next.ServeHTTP(w, r)
	})
}

// securityHeaders 统一响应头。
func (a *App) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// CSP 只作用于本服务返回的 JSON/图片(HTML 由 OpenResty 直接托管),
		// 因此不会影响管理端 TinyMCE 等依赖内联脚本的页面。
		h.Set("Content-Security-Policy",
			"default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		next.ServeHTTP(w, r)
	})
}

// accessLog 记录每个请求的方法、路径、状态、耗时与来源 IP(只信任 X-Real-IP)。
// 静态图片 2xx 噪声太大,跳过;错误与 API 调用全部记录。
func (a *App) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		path := r.URL.Path
		if status < 400 && (strings.HasPrefix(path, "/uploads/") || path == "/img") {
			return
		}
		log.Printf("http %s %s %d %dB %s ip=%s",
			r.Method, path, status, ww.BytesWritten(),
			time.Since(start).Round(time.Millisecond), a.ipHash(r))
	})
}
