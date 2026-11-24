package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fanke/backend/internal/media"
)

// 按需缩图端点 GET /img?src=<图片地址>&w=<目标宽度>
//
// 前端所有图片(卡片封面、正文内嵌图)统一指到这里:命中缓存直接返回,未命中则取源图
// → cwebp 缩到目标宽度 → 转 WebP → 落盘缓存。任何不满足转码条件的情况一律 302 回原图,
// 因此调用方无需判断能力,也不会出现打不开的图。
//
// 移动端实测收益:607KB PNG → 240w WebP 3.1KB;832KB PNG → 480w WebP 36.8KB。
// 另外 /img 是同源路径,浏览器不再需要为第三方域名做 DNS/TLS 建连。
const (
	thumbWidthMin = 32
	thumbWidthMax = 2560
	thumbFetchMax = 64 << 20 // 外链源图上限
	thumbFetchTO  = 15 * time.Second
)

// ThumbWidths 标准宽度档位(前端 srcset 与正文图注入共用同一组,避免档位漂移)。
var ThumbWidths = []int{480, 740, 1080, 1440}

// thumbWidthBuckets 允许生成缓存的宽度档位。
// 请求宽度一律向上吸附到最近档位:否则 w=33/34/35… 每个值都会落一个缓存文件,
// 一张图就能撑出上千个 WebP,既耗磁盘也耗 CPU。
var thumbWidthBuckets = []int{120, 160, 240, 320, 480, 640, 740, 960, 1080, 1280, 1440, 1600, 1920, 2560}

// snapWidth 把任意宽度向上吸附到最近的档位(不降采样,避免糊图)。
func snapWidth(w int) int {
	for _, b := range thumbWidthBuckets {
		if w <= b {
			return b
		}
	}
	return thumbWidthBuckets[len(thumbWidthBuckets)-1]
}

// errThumbSkip 该源不适合/不允许走缩图(调用方应 302 回原图)。
var errThumbSkip = errors.New("thumb: skip")

func (a *App) ServeThumb(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Vary", "Accept")
	src := strings.TrimSpace(r.URL.Query().Get("src"))
	if src == "" {
		a.fail(w, http.StatusBadRequest, "missing src")
		return
	}
	width, err := strconv.Atoi(r.URL.Query().Get("w"))
	if err != nil || width <= 0 {
		width = 740
	}
	if width < thumbWidthMin {
		width = thumbWidthMin
	}
	if width > thumbWidthMax {
		width = thumbWidthMax
	}
	width = snapWidth(width)

	// 客户端未声明支持 WebP 时直接回原图(老浏览器零风险降级;Vary: Accept 让 CDN 分开缓存)。
	if !strings.Contains(r.Header.Get("Accept"), "image/webp") || !media.Thumbable(src) || !a.mediaPluginActive(r.Context(), "image-processing") {
		a.redirectRawImage(w, r, src)
		return
	}
	w.Header().Set("Vary", "Accept")
	if a.serveMediaVariant(w, r, src, width) {
		return
	}
	file, err := a.thumbFile(r.Context(), src, width)
	if errors.Is(err, errThumbSkip) {
		a.redirectRawImage(w, r, src)
		return
	}
	if err != nil {
		// 生成失败(源不存在/源站不可达/cwebp 报错)一律回原图:
		// 客户端行为退化为"直接加载原图",与没有这套机制时完全一致,绝不出现打不开的图。
		log.Printf("thumb failed src=%s w=%d: %v", src, width, err)
		a.redirectRawImage(w, r, src)
		return
	}
	f, err := os.Open(file)
	if err != nil {
		redirectRaw(w, r, src)
		return
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || fi.Size() == 0 {
		redirectRaw(w, r, src)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/webp")
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("Vary", "Accept")
	h.Set("X-Thumb", "1")
	http.ServeContent(w, r, filepath.Base(file), time.Time{}, f)
}

// redirectRawImage 只对已知媒体源(本地/S3/白名单图床)回 302。
// 否则 /img?src=https://evil.example 会变成一个开放重定向。
func (a *App) redirectRawImage(w http.ResponseWriter, r *http.Request, src string) {
	if _, _, ok := a.thumbSource(r.Context(), src); !ok {
		http.NotFound(w, r)
		return
	}
	redirectRaw(w, r, src)
}

// redirectRaw 回原图。同源相对路径必须原样透传。
func redirectRaw(w http.ResponseWriter, r *http.Request, src string) {
	if strings.HasPrefix(src, "/") || strings.HasPrefix(src, "https://") || strings.HasPrefix(src, "http://") {
		http.Redirect(w, r, src, http.StatusFound)
		return
	}
	http.Error(w, "bad src", http.StatusBadRequest)
}

func bucketForURL(buckets []media.S3Config, src string) (media.S3Config, string, bool) {
	var best media.S3Config
	key, longest := "", 0
	for _, cfg := range buckets {
		if cfg.PublicBase == "" {
			continue
		}
		prefix := strings.TrimRight(cfg.PublicBase, "/") + "/"
		if k, ok := strings.CutPrefix(src, prefix); ok && k != "" && len(prefix) > longest {
			best, key, longest = cfg, k, len(prefix)
		}
	}
	return best, key, longest > 0
}

// thumbSource 判定图片地址属于哪类源,返回缓存标识与取源函数。
// 只有三类源可以缩图:当前 S3 桶、本地 /uploads、白名单外部图床。
func (a *App) thumbSource(ctx context.Context, src string) (string, func(context.Context) (io.ReadCloser, error), bool) {
	localPrefix := strings.TrimRight(a.Cfg.MediaBaseURL, "/") + "/"
	// 1) 当前 S3 桶:走 S3 直读(不经公网,也不吃 CDN 流量)
	if a.DB != nil {
		if settings, err := a.s3Settings(ctx); err == nil {
			if cfg, key, ok := bucketForURL(settings.Buckets, src); ok {
				if store, _, err := a.s3StoreConfig(ctx, cfg); err == nil && store != nil {
					return "s3|" + cfg.ID + "|" + cfg.Endpoint + "|" + cfg.Bucket + "|" + key, func(ctx context.Context) (io.ReadCloser, error) { return store.Open(ctx, key) }, true
				}
			}
		}
	}
	// 2) 本地媒体:按路径前缀判定(同源相对路径,或任意主机下的同路径引用)
	p := src
	if u, err := url.Parse(src); err == nil && u.Path != "" {
		p = u.Path
	}
	if key, ok := strings.CutPrefix(p, localPrefix); ok && key != "" {
		return "local|" + key, func(ctx context.Context) (io.ReadCloser, error) {
			return a.Local.Open(ctx, key)
		}, true
	}
	// 3) 白名单内的外部图床(历史配置遗留的存量外链):服务端代抓,限制主机防 SSRF
	if u, err := url.Parse(src); err == nil && u.IsAbs() && a.hostAllowed(u.Host) {
		return "ext|" + src, func(ctx context.Context) (io.ReadCloser, error) {
			return a.fetchImage(ctx, src)
		}, true
	}
	return "", nil, false
}

// thumbFile 定位/生成缩略图,返回缓存文件路径。
func (a *App) thumbFile(ctx context.Context, src string, width int) (string, error) {
	if a.Thumbs == nil {
		return "", errThumbSkip
	}
	id, open, ok := a.thumbSource(ctx, src)
	if !ok {
		return "", errThumbSkip
	}
	return a.Thumbs.Get(ctx, id, width, func(ctx context.Context) (io.ReadCloser, error) { return open(ctx) })
}

// thumbFor 生成缩图 URL;源不可缩图时返回空串(调用方保持原地址)。
// 用于正文改写:先判定能否缩图,避免写出会 302 的地址多一次往返。
func (a *App) thumbFor(ctx context.Context, src string, w int) string {
	if !a.mediaPluginActive(ctx, "image-processing") {
		return ""
	}
	if _, _, ok := a.thumbSource(ctx, src); !ok {
		return ""
	}
	return ThumbURL(src, w)
}

// hostAllowed 外部抓取主机白名单判定(含当前 S3 公开域名)。
func (a *App) hostAllowed(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "" {
		return false
	}
	for _, h := range strings.Split(a.Cfg.ThumbAllowHosts, ",") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" && h == host {
			return true
		}
	}
	if a.DB != nil {
		if cfg, err := a.DB.S3Config(context.Background()); err == nil && cfg.PublicBase != "" {
			if u, err := url.Parse(cfg.PublicBase); err == nil && strings.EqualFold(u.Host, host) {
				return true
			}
		}
	}
	return false
}

// ThumbURL 生成缩图地址(与前端 thumbUrl 同一形态)。
func ThumbURL(src string, w int) string {
	return "/img?src=" + url.QueryEscape(src) + "&w=" + strconv.Itoa(w)
}

// fetchImage 服务端代抓外部图片(仅白名单主机;限制大小与超时)。
func (a *App) fetchImage(ctx context.Context, src string) (io.ReadCloser, error) {
	ctx, cancel := context.WithTimeout(ctx, thumbFetchTO)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Accept", "image/*")
	resp, err := a.thumbClient().Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("源站返回 %d", resp.StatusCode)
	}
	if resp.ContentLength > thumbFetchMax {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("源图过大: %d 字节", resp.ContentLength)
	}
	return &cancelReadCloser{ReadCloser: resp.Body, cancel: cancel}, nil
}

type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelReadCloser) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// thumbClient 代抓外部图床时使用的客户端(每个 App 构建一次)。
// 关键:重定向目标也必须过白名单,否则白名单域名 302 到 127.0.0.1 就能打内网(SSRF)。
// 某些历史图床域名只能在本地代理后解析(公网 DNS 无记录),用 THUMB_FETCH_PROXY 指过去。
func (a *App) thumbClient() *http.Client {
	a.thumbOnce.Do(func() {
		check := func(req *http.Request, via []*http.Request) error {
			if len(via) >= 4 {
				return errors.New("too many redirects")
			}
			if !a.hostAllowed(req.URL.Host) {
				return fmt.Errorf("redirect host not allowed: %s", req.URL.Host)
			}
			return nil
		}
		transport := &http.Transport{
			Proxy:                 nil,
			DialContext:           safeProbeDial,
			MaxIdleConnsPerHost:   4,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
		}
		if raw := strings.TrimSpace(os.Getenv("THUMB_FETCH_PROXY")); raw != "" {
			if pu, err := url.Parse(raw); err == nil {
				transport = &http.Transport{
					Proxy:                 http.ProxyURL(pu),
					MaxIdleConnsPerHost:   4,
					IdleConnTimeout:       30 * time.Second,
					TLSHandshakeTimeout:   10 * time.Second,
					ResponseHeaderTimeout: 10 * time.Second,
				}
			}
		}
		a.thumbHTTP = &http.Client{Timeout: thumbFetchTO, Transport: transport, CheckRedirect: check}
	})
	return a.thumbHTTP
}
