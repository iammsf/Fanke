package migrate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	iofs "io/fs"
	neturl "net/url"
	"os"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"fanke/backend/internal/media"
	"fanke/backend/internal/netutil"
	"fanke/backend/internal/render"
	"fanke/backend/internal/repo"
)

type Options struct {
	MySQLDSN string
	SiteBase string // 如 https://example.com,用于抓取 uploads
	LocalRoot string // WP 站点根目录(如 /var/www/example.com),设置后优先本地读文件
	Storage  media.Storage
	DryRun   bool
}

type Report struct {
	PostsCreated  int
	PostsUpdated  int
	Pages         int
	Terms         int
	Comments      int
	Users         int
	MediaNew      int
	MediaDedup    int
	ManualReview  []string // 转换后仍含可疑残留的文章
}

type Runner struct {
	opt Options
	db  *repo.DB
	my  *sql.DB
	h   *http.Client
	log []string
}

func New(db *repo.DB, opt Options) *Runner {
	hc := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           netutil.SafeDialContext(""),
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}
	return &Runner{db: db, opt: opt, h: hc}
}

func (r *Runner) Run(ctx context.Context) (*Report, error) {
	my, err := sql.Open("mysql", r.opt.MySQLDSN)
	if err != nil {
		return nil, err
	}
	defer my.Close()
	if err := my.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("mysql ping: %w", err)
	}
	r.my = my

	rep := &Report{}
	userMap, err := r.migrateUsers(ctx, rep)
	if err != nil {
		return nil, err
	}
	termMap, err := r.migrateTerms(ctx, rep)
	if err != nil {
		return nil, err
	}
	postMap, err := r.migratePosts(ctx, userMap, termMap, rep)
	if err != nil {
		return nil, err
	}
	if err := r.migrateComments(ctx, postMap, rep); err != nil {
		return nil, err
	}
	rep.ManualReview = r.log
	return rep, nil
}

// ---------- users ----------

func (r *Runner) migrateUsers(ctx context.Context, rep *Report) (map[int64]int64, error) {
	rows, err := r.my.QueryContext(ctx, `SELECT ID, user_login, user_email, display_name FROM wp_users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var id int64
		var login, email, name string
		if err := rows.Scan(&id, &login, &email, &name); err != nil {
			return nil, err
		}
		var existing int64
		err := r.db.QueryRow(ctx, `SELECT id FROM users WHERE email=$1`, email).Scan(&existing)
		if err == nil {
			out[id] = existing
			continue
		}
		if r.opt.DryRun {
			out[id] = -1
			rep.Users++
			continue
		}
		// WP 的 phpass 哈希无法转换,置随机密码并禁用登录,由引导/重置流程接管
		newID, err := r.db.CreateUser(ctx, email, login, "!migrated-no-pass!", "author", name)
		if err != nil {
			return nil, fmt.Errorf("create user %s: %w", login, err)
		}
		out[id] = newID
		rep.Users++
	}
	return out, rows.Err()
}

// ---------- terms ----------

func (r *Runner) migrateTerms(ctx context.Context, rep *Report) (map[int64]repo.TermRef, error) {
	rows, err := r.my.QueryContext(ctx, `SELECT t.term_id, t.name, t.slug, tt.taxonomy
		FROM wp_terms t JOIN wp_term_taxonomy tt ON tt.term_id=t.term_id
		WHERE tt.taxonomy IN ('category','post_tag')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]repo.TermRef{}
	for rows.Next() {
		var id int64
		var name, slug, tax string
		if err := rows.Scan(&id, &name, &slug, &tax); err != nil {
			return nil, err
		}
		typ := "tag"
		if tax == "category" {
			typ = "category"
		}
		if slug == "" {
			slug = name
		}
		out[id] = repo.TermRef{Name: name, Slug: slug}
		rep.Terms++
		if r.opt.DryRun {
			continue
		}
		if _, err := r.db.EnsureTerm(ctx, name, slug, typ); err != nil {
			return nil, err
		}
	}
	return out, rows.Err()
}

// ---------- posts ----------

var imgRe = regexp.MustCompile(`src="([^"]*wp-content/uploads/[^"]*)"`)

type wpPost struct {
	ID             int64
	Slug           string
	Title          string
	Excerpt        string
	Content        string
	Date           time.Time
	Author         int64
	PostType       string
	CommentStatus  string
}

func (r *Runner) migratePosts(ctx context.Context, userMap map[int64]int64, termMap map[int64]repo.TermRef, rep *Report) (map[int64]int64, error) {
	rows, err := r.my.QueryContext(ctx, `SELECT ID, post_name, post_title, post_excerpt, post_content,
		post_date, post_author, post_type, comment_status
		FROM wp_posts WHERE post_status='publish' AND post_type IN ('post','page') ORDER BY post_date`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	postMap := map[int64]int64{}
	for rows.Next() {
		var p wpPost
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &p.Excerpt, &p.Content,
			&p.Date, &p.Author, &p.PostType, &p.CommentStatus); err != nil {
			return nil, err
		}
		if p.Slug == "" {
			p.Slug = fmt.Sprintf("post-%d", p.ID)
		}
		// WP 中文 slug 以 %xx 编码形态存库,统一解码为原文,匹配时由 API 层再解码 URL 参数
		if decoded, err := neturl.PathUnescape(p.Slug); err == nil {
			p.Slug = decoded
		}
		md, err := r.convert(ctx, p.Content, p.Slug, rep)
		if err != nil {
			return nil, fmt.Errorf("post %d: %w", p.ID, err)
		}
		htmlOut, err := render.Markdown(md)
		if err != nil {
			return nil, err
		}
		cats, tags, err := r.termsFor(ctx, p.ID, termMap)
		if err != nil {
			return nil, err
		}
		author := userMap[p.Author]
		if author == 0 {
			author = 1 // 兜底给主账号
		}
		if r.opt.DryRun {
			postMap[p.ID] = -1
			if p.PostType == "page" {
				rep.Pages++
			} else {
				rep.PostsCreated++
			}
			continue
		}
		var existingID int64
		err = r.db.QueryRow(ctx, `SELECT id FROM posts WHERE slug=$1`, p.Slug).Scan(&existingID)
		in := repo.PostInput{
			Slug: p.Slug, Title: p.Title, Summary: strings.TrimSpace(p.Excerpt),
			ContentMD: md, ContentHTML: htmlOut, Status: "published",
			AuthorID: author, AllowComments: p.CommentStatus == "open", IsPublic: true,
			PublishedAt: &p.Date, Categories: cats, Tags: tags,
		}
		if err == nil { // 幂等:已存在则更新
			if err := r.db.UpdatePost(ctx, existingID, in); err != nil {
				return nil, err
			}
			postMap[p.ID] = existingID
			rep.PostsUpdated++
		} else {
			newID, err := r.db.CreatePost(ctx, in)
			if err != nil {
				return nil, err
			}
			postMap[p.ID] = newID
			if p.PostType == "page" {
				rep.Pages++
			} else {
				rep.PostsCreated++
			}
		}
	}
	return postMap, rows.Err()
}

func (r *Runner) termsFor(ctx context.Context, postID int64, termMap map[int64]repo.TermRef) (cats, tags []repo.TermRef, err error) {
	rows, err := r.my.QueryContext(ctx, `SELECT tt.term_id, tt.taxonomy FROM wp_term_relationships tr
		JOIN wp_term_taxonomy tt ON tt.term_taxonomy_id=tr.term_taxonomy_id WHERE tr.object_id=?`, postID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var tid int64
		var tax string
		if err := rows.Scan(&tid, &tax); err != nil {
			return nil, nil, err
		}
		ref, ok := termMap[tid]
		if !ok {
			continue
		}
		if tax == "category" {
			cats = append(cats, ref)
		} else {
			tags = append(tags, ref)
		}
	}
	return cats, tags, rows.Err()
}

// convert 抓取并替换 uploads 图片,然后转 Markdown。
func (r *Runner) convert(ctx context.Context, content, slug string, rep *Report) (string, error) {
	if r.opt.DryRun {
		// 干跑:只统计图片数,不下载不写入
		rep.MediaNew += len(imgRe.FindAllString(content, -1))
		return HTMLToMarkdown(content), nil
	}
	seen := map[string]string{}
	out := imgRe.ReplaceAllStringFunc(content, func(m string) string {
		g := imgRe.FindStringSubmatch(m)
		src := g[1]
		if newURL, ok := seen[src]; ok {
			return `src="` + newURL + `"`
		}
		newURL, dedup, err := r.transferImage(ctx, src)
		if err != nil {
			r.log = append(r.log, fmt.Sprintf("[%s] 图片迁移失败 %s: %v", slug, src, err))
			return m // 保留原地址
		}
		seen[src] = newURL
		if dedup {
			rep.MediaDedup++
		} else {
			rep.MediaNew++
		}
		return `src="` + newURL + `"`
	})

	md := HTMLToMarkdown(out)
	// 残留检查跑在转换后的 Markdown 上
	if strings.Contains(md, "wp-block") || strings.Contains(md, "enlighter") {
		r.log = append(r.log, fmt.Sprintf("[%s] Markdown 含区块/插件残留,建议人工复核", slug))
	}
	if strings.Contains(md, "wp-content/uploads") {
		r.log = append(r.log, fmt.Sprintf("[%s] 仍含 uploads 链接,需人工复核", slug))
	}
	return md, nil
}

var sizeSuffixRe = regexp.MustCompile(`-\d+x\d+(\.\w+)$`)

// fetchBytes 下载图片,遇 404 时尝试去掉 WP 缩略图尺寸后缀取原图。
func (r *Runner) fetchBytes(ctx context.Context, url string) ([]byte, error) {
	data, status, err := r.httpGet(ctx, url)
	if err == nil {
		return data, nil
	}
	if status == 404 {
		if m := sizeSuffixRe.FindStringSubmatch(url); m != nil {
			orig := sizeSuffixRe.ReplaceAllString(url, m[1])
			if orig != url {
				data, _, err2 := r.httpGet(ctx, orig)
				if err2 == nil {
					return data, nil
				}
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("status %d", status)
}

// safeLocalFilePath 将 URL 路径安全解析到 LocalRoot 目录内,彻底防止 .. 与同前缀目录逃逸。
func safeLocalFilePath(localRoot, urlPath string) (string, error) {
	decoded, err := neturl.PathUnescape(urlPath)
	if err != nil {
		return "", fmt.Errorf("invalid path")
	}
	if strings.ContainsAny(decoded, "\x00\\") {
		return "", fmt.Errorf("path escape")
	}
	rootAbs, err := filepath.Abs(filepath.Clean(localRoot))
	if err != nil {
		return "", fmt.Errorf("path escape")
	}
	rel := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(decoded, "/")))
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path escape")
	}
	fp := filepath.Join(rootAbs, rel)
	if !strings.HasPrefix(fp, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("path escape")
	}
	return fp, nil
}

func (r *Runner) httpGet(ctx context.Context, url string) ([]byte, int, error) {
	// 本地根目录优先:URL 路径映射到磁盘文件
	if r.opt.LocalRoot != "" {
		if p := strings.TrimPrefix(url, r.opt.SiteBase); strings.HasPrefix(p, "/") {
			fp, err := safeLocalFilePath(r.opt.LocalRoot, p)
			if err != nil {
				return nil, 0, err
			}
			if b, err := os.ReadFile(fp); err == nil {
				return b, 200, nil
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "fanke-migrator/1.0")
	resp, err := r.h.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	return data, resp.StatusCode, err
}

// transferImage 下载(或本地读取)原图→去重→存入目标存储,返回新 URL 与是否去重命中。
func (r *Runner) transferImage(ctx context.Context, src string) (string, bool, error) {
	url := src
	if strings.HasPrefix(url, "/") {
		url = r.opt.SiteBase + url
	}
	if strings.Contains(url, "?") {
		url = url[:strings.Index(url, "?")] // 去掉 size 查询参数取原图
	}
	data, err := r.fetchBytes(ctx, url)
	if err != nil {
		return "", false, err
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	if dup, err := r.db.MediaBySHA(ctx, sha); err == nil && dup != nil {
		return dup.URL, true, nil // 去重命中
	}
	ext := ".jpg"
	if i := strings.LastIndex(url, "."); i > 0 {
		e := strings.ToLower(url[i:])
		if len(e) <= 6 {
			ext = e
		}
	}
	key := "wp-migrate/" + sha[:16] + ext
	obj, err := r.opt.Storage.Save(ctx, key, strings.NewReader(string(data)), int64(len(data)), mimeOfExt(ext))
	if err != nil {
		return "", false, err
	}
	if _, err := r.db.CreateMedia(ctx, repo.Media{
		Filename: lastSeg(url), Storage: r.opt.Storage.Name(), Key: obj.Key, URL: obj.URL,
		MIME: obj.MIME, Size: obj.Size, SHA256: sha,
	}); err != nil {
		return "", false, err
	}
	return obj.URL, false, nil
}

// ---------- comments ----------

func (r *Runner) migrateComments(ctx context.Context, postMap map[int64]int64, rep *Report) error {
	rows, err := r.my.QueryContext(ctx, `SELECT comment_ID, comment_post_ID, comment_parent,
		comment_author, comment_author_email, comment_content, comment_date
		FROM wp_comments WHERE comment_approved='1' ORDER BY comment_date`)
	if err != nil {
		return err
	}
	defer rows.Close()
	idMap := map[int64]int64{}
	for rows.Next() {
		var cid, pid, parent int64
		var author, email, content string
		var date time.Time
		if err := rows.Scan(&cid, &pid, &parent, &author, &email, &content, &date); err != nil {
			return err
		}
		if r.opt.DryRun {
			rep.Comments++
			continue
		}
		newPost, ok := postMap[pid]
		if !ok || newPost <= 0 {
			continue
		}
		var newParent *int64
		if parent > 0 {
			if np, ok := idMap[parent]; ok {
				newParent = &np
			}
		}
		nid, err := r.db.CreateComment(ctx, repo.Comment{
			PostID: newPost, ParentID: newParent, AuthorName: author, AuthorEmail: email,
			Content: render.SanitizeHTML(content), Status: "approved", CreatedAt: date,
		})
		if err != nil {
			return err
		}
		idMap[cid] = nid
		rep.Comments++
	}
	return rows.Err()
}

func lastSeg(u string) string {
	u = strings.Split(u, "?")[0]
	if i := strings.LastIndex(u, "/"); i >= 0 {
		return u[i+1:]
	}
	return u
}

// localExtOK 本地副本导入允许的扩展名(与后端上传白名单一致)。
var localExtOK = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
	".svg": true, ".avif": true, ".mp4": true, ".pdf": true,
}

// ImportLocal 把 LocalRoot/wp-content/uploads 的文件复制进本地卷并登记为 local 媒体(冗余备份,不改文章引用)。
func (r *Runner) ImportLocal(ctx context.Context, store media.Storage) (added, skipped int, err error) {
	base := filepath.Join(r.opt.LocalRoot, "wp-content", "uploads")
	walkErr := filepath.WalkDir(base, func(p string, d iofs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		ext := strings.ToLower(filepath.Ext(p))
		if !localExtOK[ext] {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(data)
		sha := hex.EncodeToString(sum[:])
		if has, _ := r.db.HasLocalMedia(ctx, sha); has {
			skipped++
			return nil
		}
		rel, _ := filepath.Rel(base, p)
		key := filepath.ToSlash(rel)
		obj, err := store.Save(ctx, key, bytes.NewReader(data), int64(len(data)), mimeOfExt(ext))
		if err != nil {
			return fmt.Errorf("save %s: %w", key, err)
		}
		w, h := 0, 0
		if cfg, _, e := image.DecodeConfig(bytes.NewReader(data)); e == nil {
			w, h = cfg.Width, cfg.Height
		}
		if _, err := r.db.CreateMedia(ctx, repo.Media{
			Filename: filepath.Base(p), Storage: store.Name(), Key: obj.Key, URL: obj.URL,
			MIME: obj.MIME, Size: obj.Size, Width: w, Height: h, SHA256: sha,
		}); err != nil {
			return fmt.Errorf("db %s: %w", key, err)
		}
		added++
		return nil
	})
	if walkErr != nil {
		return added, skipped, fmt.Errorf("walk uploads: %w", walkErr)
	}
	return added, skipped, nil
}

func mimeOfExt(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	}
	return "image/jpeg"
}
