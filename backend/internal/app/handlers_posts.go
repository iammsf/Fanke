package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"fanke/backend/internal/render"
	"fanke/backend/internal/repo"
)

// routeSlug 还原 URL 编码的 slug(WP 中文 slug 存的是 %xx 形态,统一解码后匹配库内中文原文)。
func routeSlug(r *http.Request) string {
	s := chi.URLParam(r, "slug")
	if d, err := url.PathUnescape(s); err == nil {
		return d
	}
	return s
}

// visiblePost 未公开文章的可见性:已公开,或请求携带有效登录凭证且具备该文章的编辑权限(管理员或作者本人)。
// 不能信任 X-Fanke-Internal 之类的自定义头——任何 HTTP 客户端都能伪造。
func visiblePost(r *http.Request, p *repo.Post) bool {
	if p.IsPublic {
		return true
	}
	u := currentUser(r)
	return u.ID != 0 && canEditPost(u, p.AuthorID)
}

func (a *App) defaultCovers(ctx context.Context) map[string]string {
	def, _ := cachedPublicJSON(ctx, &a.publicCache, cacheDefaultCovers, func() (map[string]string, error) {
		return a.loadDefaultCovers(ctx)
	})
	return def
}

func (a *App) loadDefaultCovers(ctx context.Context) (map[string]string, error) {
	raw, err := a.DB.GetSetting(ctx, "default_covers")
	if err != nil {
		return nil, err
	}
	var def map[string]string
	err = json.Unmarshal(raw, &def)
	return def, err
}

func coverOrDefault(p *repo.Post, def map[string]string) {
	if p.CoverURL != "" || len(def) == 0 {
		return
	}
	if u := def[p.Type]; u != "" {
		p.CoverURL = u
	} else if u := def["standard"]; u != "" {
		p.CoverURL = u
	}
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v)
}

// slug 兼容 WordPress 迁移来的历史 slug，仍禁止 ".." 防路径穿越。
var slugRe = regexp.MustCompile(`^[a-zA-Z0-9%_\-.~\x{4e00}-\x{9fff}\x{3000}-\x{303f}\x{ff01}-\x{ff65}\x{2600}-\x{27bf}\x{1f300}-\x{1faff}]+$`)

func validSlug(s string) bool {
	return s != "" && len(s) <= 200 && slugRe.MatchString(s) && !strings.Contains(s, "..")
}

type postReq struct {
	Slug          string         `json:"slug"`
	Title         string         `json:"title"`
	Summary       string         `json:"summary"`
	ContentMD     string         `json:"content_md"`
	ContentHTML   string         `json:"content_html"`
	Status        string         `json:"status"`
	Type          string         `json:"type"`
	CoverMediaID  *int64         `json:"cover_media_id"`
	AllowComments *bool          `json:"allow_comments"`
	IsPublic      *bool          `json:"is_public"`
	PublishedAt   *string        `json:"published_at"`
	Categories    []repo.TermRef `json:"categories"`
	Tags          []repo.TermRef `json:"tags"`
}

func normPostType(t string) string {
	if t == "moment" {
		return "moment"
	}
	return "standard"
}

func (a *App) ListPostsPublic(w http.ResponseWriter, r *http.Request) {
	q := parsePostQuery(r, "published")
	type homeSort struct {
		Field string `json:"field"`
		Order string `json:"order"`
	}
	hs, _ := cachedPublicJSON(r.Context(), &a.publicCache, cacheHomeSort, func() (homeSort, error) {
		var value homeSort
		raw, err := a.DB.GetSetting(r.Context(), "home_sort")
		if err != nil {
			return value, err
		}
		err = json.Unmarshal(raw, &value)
		return value, err
	})
	switch hs.Field {
	case "words", "type":
		q.SortField = hs.Field
	default:
		q.SortField = "date"
	}
	if hs.Order == "asc" {
		q.SortOrder = "asc"
	}
	a.renderPostList(w, r, q, true)
}

func (a *App) ListPostsAdmin(w http.ResponseWriter, r *http.Request) {
	q := parsePostQuery(r, r.URL.Query().Get("status"))
	q.PublicOnly = false
	if u := currentUser(r); u.Role != "admin" {
		q.AuthorID = u.ID
	}
	a.renderPostList(w, r, q, false)
}

func parsePostQuery(r *http.Request, status string) repo.PostQuery {
	u := r.URL.Query()
	page, _ := strconv.Atoi(u.Get("page"))
	per, _ := strconv.Atoi(u.Get("per"))
	return repo.PostQuery{Status: status, PublicOnly: true, Tag: u.Get("tag"), Category: u.Get("category"), Q: u.Get("q"), Page: page, Per: per}
}

func (a *App) renderPostList(w http.ResponseWriter, r *http.Request, q repo.PostQuery, cacheable bool) {
	items, total, err := a.DB.ListPosts(r.Context(), q)
	if err != nil {
		a.failInternal(w, r, "list posts", err)
		return
	}
	for i := range items {
		items[i].ContentMD = ""
		items[i].ContentHTML = ""
	}
	if q.PublicOnly {
		def := a.defaultCovers(r.Context())
		for i := range items {
			coverOrDefault(&items[i], def)
		}
	}
	payload := map[string]any{"items": items, "total": total, "page": q.Page, "per": q.Per}
	if cacheable {
		writeJSONCached(w, r, http.StatusOK, payload)
		return
	}
	// 管理端文章列表含私密草稿且按角色隔离,禁止覆盖为 public 缓存。
	writeJSON(w, http.StatusOK, payload)
}

func (a *App) GetPost(w http.ResponseWriter, r *http.Request) {
	slug := routeSlug(r)
	p, err := a.DB.PostBySlug(r.Context(), slug, true, false)
	if errors.Is(err, repo.ErrNotFound) {
		a.fail(w, http.StatusNotFound, "post not found")
		return
	}
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !visiblePost(r, p) {
		a.fail(w, http.StatusNotFound, "post not found")
		return
	}
	// 浏览量异步累加:必须用独立 context,请求返回后 r.Context() 会被取消,UPDATE 会被中断。
	go func(id int64) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		a.DB.IncrViews(ctx, id)
	}(p.ID)
	n, _ := a.DB.CommentCountForPost(r.Context(), p.ID)
	coverOrDefault(p, a.defaultCovers(r.Context()))
	a.enrichPostImages(r.Context(), p)
	out := map[string]any{"post": p, "comments": n}
	if p.IsPublic {
		prev, next, err := a.DB.AdjacentPublicPosts(r.Context(), p.ID, postRefTime(p))
		if err != nil {
			log.Printf("adjacent posts lookup failed: %v", err)
		} else {
			out["prev"] = prev
			out["next"] = next
		}
		writeJSONCached(w, r, http.StatusOK, out)
		return
	}
	// 草稿预览:响应带登录态,绝不能进入公共缓存/CDN,且不返回邻篇。
	writeJSON(w, http.StatusOK, out)
}

func postRefTime(p *repo.Post) time.Time {
	if p.PublishedAt != nil {
		return *p.PublishedAt
	}
	return p.CreatedAt
}

// enrichPostImages 只在读取接口改写 HTML，不落库。
func (a *App) enrichPostImages(ctx context.Context, p *repo.Post) {
	if p == nil || p.ContentHTML == "" || a.DB == nil {
		return
	}
	srcs := render.ImageSrcs(p.ContentHTML)
	if len(srcs) == 0 {
		return
	}
	dims, err := a.DB.MediaDimsByURLs(ctx, srcs)
	if err != nil {
		log.Printf("enrich images: dims lookup failed: %v", err)
	}
	p.ContentHTML = render.RewriteImages(p.ContentHTML, render.ImgRewrite{
		Thumb: func(src string, w int) string { return a.thumbFor(ctx, src, w) },
		Size: func(src string) (int, int) {
			if d, ok := dims[src]; ok {
				return d[0], d[1]
			}
			return 0, 0
		},
		Widths: ThumbWidths,
	})
}

func (a *App) GetPostAdmin(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "bad id")
		return
	}
	p, ok := a.requireOwnPost(w, r, id)
	if !ok {
		return
	}
	full, err := a.DB.PostBySlug(r.Context(), p.Slug, false, false)
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, full)
}

func (a *App) CreatePost(w http.ResponseWriter, r *http.Request) {
	var req postReq
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Title == "" {
		a.fail(w, http.StatusBadRequest, "标题不能为空")
		return
	}
	if !validSlug(req.Slug) {
		log.Printf("post save: invalid slug %q (len=%d)", req.Slug, len(req.Slug))
		bad := ""
		for _, c := range req.Slug {
			if !slugRe.MatchString(string(c)) {
				bad = fmt.Sprintf("U+%04X", c)
				break
			}
		}
		if bad == "" {
			bad = "(空或超长或含 ..)"
		}
		a.fail(w, http.StatusBadRequest, "slug 含非法字符 "+bad)
		return
	}
	if req.Status != "draft" && req.Status != "published" {
		req.Status = "draft"
	}
	htmlOut, contentMD := req.ContentHTML, req.ContentMD
	if strings.TrimSpace(htmlOut) == "" {
		var err error
		htmlOut, err = renderPostMarkdown(contentMD)
		if err != nil {
			a.fail(w, http.StatusInternalServerError, "render failed")
			return
		}
	} else {
		contentMD = ""
	}
	in := repo.PostInput{
		Slug: req.Slug, Title: req.Title, Summary: req.Summary,
		ContentMD: contentMD, ContentHTML: htmlOut, Status: req.Status, Type: normPostType(req.Type),
		AuthorID: currentUser(r).ID, CoverMediaID: req.CoverMediaID,
		AllowComments: req.AllowComments == nil || *req.AllowComments,
		IsPublic:      req.IsPublic == nil || *req.IsPublic,
		Categories:    req.Categories, Tags: req.Tags,
	}
	if req.PublishedAt != nil {
		if t, err := parseTime(*req.PublishedAt); err == nil {
			in.PublishedAt = &t
		}
	}
	if req.Status == "published" && in.PublishedAt == nil {
		now := nowUTC()
		in.PublishedAt = &now
	}
	if err := preparePostSave(r.Context(), BeforePostCreate, 0, &in, nil, a.dispatchLifecycle); err != nil {
		a.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	id, err := a.DB.CreatePost(r.Context(), in)
	if err != nil {
		if isUnique(err) {
			a.fail(w, http.StatusConflict, "slug exists")
			return
		}
		a.fail(w, http.StatusInternalServerError, "create failed")
		return
	}
	_ = a.dispatchLifecycle(r.Context(), AfterPostCreated, &LifecycleData{PostID: id, Post: &in, ContentHTML: in.ContentHTML})
	p, _ := a.DB.PostByID(r.Context(), id)
	writeJSON(w, http.StatusCreated, p)
}

func (a *App) UpdatePost(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "bad id")
		return
	}
	old, ok := a.requireOwnPost(w, r, id)
	if !ok {
		return
	}
	oldFull, err := a.DB.PostBySlug(r.Context(), old.Slug, false, false)
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	var req postReq
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Title == "" {
		a.fail(w, http.StatusBadRequest, "标题不能为空")
		return
	}
	if !validSlug(req.Slug) {
		log.Printf("post save: invalid slug %q (len=%d)", req.Slug, len(req.Slug))
		bad := ""
		for _, c := range req.Slug {
			if !slugRe.MatchString(string(c)) {
				bad = fmt.Sprintf("U+%04X", c)
				break
			}
		}
		if bad == "" {
			bad = "(空或超长或含 ..)"
		}
		a.fail(w, http.StatusBadRequest, "slug 含非法字符 "+bad)
		return
	}
	if req.Status != "draft" && req.Status != "published" {
		req.Status = old.Status
	}
	htmlOut, contentMD := req.ContentHTML, req.ContentMD
	if strings.TrimSpace(htmlOut) == "" {
		var err error
		htmlOut, err = renderPostMarkdown(contentMD)
		if err != nil {
			a.fail(w, http.StatusInternalServerError, "render failed")
			return
		}
	} else {
		// HTML 编辑器不会回传旧 Markdown，保留历史正文。
		contentMD = oldFull.ContentMD
	}
	in := repo.PostInput{
		Slug: req.Slug, Title: req.Title, Summary: req.Summary,
		ContentMD: contentMD, ContentHTML: htmlOut, Status: req.Status, Type: normPostType(req.Type),
		AuthorID: old.AuthorID, CoverMediaID: req.CoverMediaID,
		AllowComments: req.AllowComments == nil || *req.AllowComments,
		IsPublic:      req.IsPublic == nil || *req.IsPublic,
		Categories:    req.Categories, Tags: req.Tags,
	}
	if req.PublishedAt != nil {
		if t, err := parseTime(*req.PublishedAt); err == nil {
			in.PublishedAt = &t
		}
	}
	if req.Status == "published" && in.PublishedAt == nil {
		now := nowUTC()
		in.PublishedAt = &now
	}
	if err := preparePostSave(r.Context(), BeforePostUpdate, id, &in, oldFull, a.dispatchLifecycle); err != nil {
		a.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := a.DB.UpdatePost(r.Context(), id, in); err != nil {
		if isUnique(err) {
			a.fail(w, http.StatusConflict, "slug exists")
			return
		}
		a.fail(w, http.StatusInternalServerError, "update failed")
		return
	}
	_ = a.dispatchLifecycle(r.Context(), AfterPostUpdated, &LifecycleData{PostID: id, Post: &in, OldPost: oldFull, ContentHTML: in.ContentHTML})
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) DeletePost(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if _, ok := a.requireOwnPost(w, r, id); !ok {
		return
	}
	if err := a.deletePostWithHooks(r.Context(), id); err != nil {
		var rejected rejectedError
		switch {
		case errors.Is(err, repo.ErrNotFound):
			a.fail(w, http.StatusNotFound, "not found")
		case errors.As(err, &rejected):
			a.fail(w, http.StatusUnprocessableEntity, rejected.Error())
		default:
			a.fail(w, http.StatusInternalServerError, "delete failed")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SetPostPublic 列表页快捷切换公开性:仅更新 is_public，不触发正文保存生命周期。
func (a *App) SetPostPublic(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if _, ok := a.requireOwnPost(w, r, id); !ok {
		return
	}
	var req struct {
		IsPublic bool `json:"is_public"`
	}
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := a.DB.SetPostPublic(r.Context(), id, req.IsPublic); err != nil {
		a.fail(w, http.StatusInternalServerError, "update failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) PublishPost(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if _, ok := a.requireOwnPost(w, r, id); !ok {
		return
	}
	if err := a.DB.PublishPost(r.Context(), id); err != nil {
		a.fail(w, http.StatusInternalServerError, "publish failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) SetPostSticky(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if _, ok := a.requireOwnPost(w, r, id); !ok {
		return
	}
	var req struct {
		Sticky bool `json:"sticky"`
	}
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := a.DB.SetPostSticky(r.Context(), id, req.Sticky); err != nil {
		a.fail(w, http.StatusInternalServerError, "update failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) ListStickyPosts(w http.ResponseWriter, r *http.Request) {
	var scope int64
	if u := currentUser(r); u.Role != "admin" {
		scope = u.ID
	}
	items, err := a.DB.ListStickyPosts(r.Context(), scope)
	if err != nil {
		a.failInternal(w, r, "list sticky", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *App) SetStickyOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(req.IDs) > 500 {
		a.fail(w, http.StatusBadRequest, "too many")
		return
	}
	if err := a.DB.SetStickyOrder(r.Context(), req.IDs); err != nil {
		a.fail(w, http.StatusInternalServerError, "save failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
