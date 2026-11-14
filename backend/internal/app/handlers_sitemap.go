package app

import (
	"encoding/xml"
	"net/http"
	"strings"
	"time"
)

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
	scheme := "https"
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = strings.Split(p, ",")[0]
	}
	base := strings.TrimRight(scheme+"://"+r.Host, "/")

	type url struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod,omitempty"`
	}
	type urlset struct {
		XMLName xml.Name `xml:"urlset"`
		Xmlns   string   `xml:"xmlns,attr"`
		URLs    []url    `xml:"url"`
	}
	set := urlset{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	set.URLs = append(set.URLs, url{Loc: base + "/"})
	for _, e := range entries {
		set.URLs = append(set.URLs, url{
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
	w.Header().Set("Cache-Control", "public, max-age=600")
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(out)
}
