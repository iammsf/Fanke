package app

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// validHostHeader 严格校验 Host / origin 主机名:拒绝空白、控制字符、分号与路径/凭据注入。
func validHostHeader(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if c <= 0x20 || c >= 0x7f || strings.ContainsRune(";/\\?#@,\"'`", rune(c)) {
			return false
		}
	}
	u, err := url.Parse("http://" + h)
	if err != nil || u.Host != h || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Hostname() == "" {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return true
}

// safeForwardedProto 仅采纳合法的 http/https X-Forwarded-Proto 值,防止协议头注入。
func safeForwardedProto(r *http.Request, fallback string) string {
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		first := strings.ToLower(strings.TrimSpace(strings.Split(p, ",")[0]))
		if first == "http" || first == "https" {
			return first
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return fallback
}

// extensionPageOrigins 返回插件沙箱 iframe 允许加载同站资源的显式 origin。
// 去除 allow-same-origin 后 iframe 变为 opaque origin,CSP 中的 'self' 不再匹配主站 URL,
// 因此需拼入显式 origin 保证 ./app.js 与 assets/init.js 可正常加载。
func (a *App) extensionPageOrigins(r *http.Request) string {
	if a.Cfg != nil {
		if raw := strings.TrimSpace(a.Cfg.SiteURL); raw != "" {
			if u, err := url.Parse(raw); err == nil && (u.Scheme == "http" || u.Scheme == "https") && validHostHeader(u.Host) {
				return u.Scheme + "://" + u.Host
			}
		}
	}
	host := strings.TrimSpace(r.Host)
	if !validHostHeader(host) {
		return ""
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		first := strings.ToLower(strings.TrimSpace(strings.Split(p, ",")[0]))
		if first == "http" || first == "https" {
			return first + "://" + host
		}
	}
	if r.TLS != nil {
		return "https://" + host
	}
	return "https://" + host + " http://" + host
}

// SitemapXML 输出 sitemap.xml。
// 静态站点没有构建期路由清单,所以由后端按库中已发布文章实时生成,
// 再由 OpenResty 把 /sitemap.xml 反代过来(robots.txt 里已声明该地址)。
func (a *App) SitemapXML(w http.ResponseWriter, r *http.Request) {
	if a.DB == nil {
		http.Error(w, "not configured", http.StatusServiceUnavailable)
		return
	}
	entries, err := a.DB.SitemapPosts(r.Context())
	if err != nil {
		a.failInternal(w, r, "sitemap", err)
		return
	}
	// 配了 SITE_URL 就用它:Host 头由客户端控制,前面若挂了不按 Host 分键的缓存,
	// 伪造 Host 就能把 sitemap 里所有 <loc> 改成攻击者的域名(缓存投毒)。
	base := strings.TrimRight(a.Cfg.SiteURL, "/")
	configuredSiteURL := base != ""
	if !configuredSiteURL {
		scheme := safeForwardedProto(r, "https")
		host := strings.TrimSpace(r.Host)
		if !validHostHeader(host) {
			a.fail(w, http.StatusBadRequest, "invalid host")
			return
		}
		base = strings.TrimRight(scheme+"://"+host, "/")
	}

	type sitemapURL struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod,omitempty"`
	}
	type urlset struct {
		XMLName xml.Name     `xml:"urlset"`
		Xmlns   string       `xml:"xmlns,attr"`
		URLs    []sitemapURL `xml:"url"`
	}
	set := urlset{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	set.URLs = append(set.URLs, sitemapURL{Loc: base + "/"})
	for _, e := range entries {
		set.URLs = append(set.URLs, sitemapURL{
			Loc:     base + "/posts/" + e.Slug,
			LastMod: e.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	out, err := xml.MarshalIndent(set, "", "  ")
	if err != nil {
		a.failInternal(w, r, "sitemap encode", err)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	if configuredSiteURL {
		w.Header().Set("Cache-Control", "public, max-age=600")
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(out)
}
