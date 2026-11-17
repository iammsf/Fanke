package repo

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type PostQuery struct {
	Status     string // published/draft/""(管理端全部)
	PublicOnly bool   // 仅公开文章(前端接口用)
	Tag        string
	Category   string
	Q          string
	AuthorID   int64 // 仅该作者的文章(后台 author 视角);0 表示不过滤
	Page       int
	Per        int
	// 首页排序:仅 Q=="" 且无 tag/category 过滤时生效(搜索/归档页保持相关度/默认序)
	SortField string // date/words/type(views 由上层兜底为 date)
	SortOrder string // asc/desc
}

const postCols = `p.id, p.slug, p.title, p.summary, p.status, p.author_id, p.cover_media_id,
	p.allow_comments, p.is_public, p.views, p.type, p.sticky, p.published_at, p.created_at, p.updated_at, u.display_name AS author_name`

func scanPost(row pgx.Row) (*Post, error) {
	p := &Post{}
	err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Summary, &p.Status, &p.AuthorID, &p.CoverMediaID,
		&p.AllowComments, &p.IsPublic, &p.Views, &p.Type, &p.Sticky, &p.PublishedAt, &p.CreatedAt, &p.UpdatedAt, &p.AuthorName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func (db *DB) ListPosts(ctx context.Context, q PostQuery) ([]Post, int, error) {
	var where []string
	args := []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.Replace(cond, "$?", "$"+itoa(len(args)), 1))
	}

	switch q.Status {
	case "published":
		where = append(where, `p.status='published'`)
	case "draft":
		where = append(where, `p.status='draft'`)
	}
	if q.PublicOnly {
		where = append(where, `p.is_public`)
	}
	if q.AuthorID != 0 {
		add(`p.author_id=$?`, q.AuthorID)
	}
	if q.Tag != "" {
		add(`t1.slug=$? AND t1.type='tag'`, q.Tag)
	}
	if q.Category != "" {
		add(`t2.slug=$? AND t2.type='category'`, q.Category)
	}
	if q.Q != "" {
		args = append(args, "%"+q.Q+"%")
		where = append(where, `(p.title ILIKE $`+itoa(len(args))+` OR p.summary ILIKE $`+itoa(len(args))+` OR p.content_md ILIKE $`+itoa(len(args))+`)`)
	}

	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	base := ` FROM posts p JOIN users u ON u.id=p.author_id`
	base += ` LEFT JOIN media m ON m.id=p.cover_media_id` // 列表卡片需要封面图
	if q.Tag != "" {
		base += ` JOIN post_terms pt1 ON pt1.post_id=p.id JOIN terms t1 ON t1.id=pt1.term_id`
	}
	if q.Category != "" {
		base += ` JOIN post_terms pt2 ON pt2.post_id=p.id JOIN terms t2 ON t2.id=pt2.term_id`
	}

	var total int
	if err := db.QueryRow(ctx, `SELECT count(DISTINCT p.id) `+base+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if q.Per <= 0 || q.Per > 100 {
		q.Per = 10
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	args = append(args, q.Per, (q.Page-1)*q.Per)
	// 内层 DISTINCT ON 去重(单 term 过滤实际不会产生重复,保留以策安全),外层负责真正排序
	order := `published_at DESC NULLS LAST, id DESC` // 搜索/归档/管理端:保持默认新→旧
	if q.Q == "" && q.Tag == "" && q.Category == "" && q.PublicOnly {
		dir := "DESC"
		if q.SortOrder == "asc" {
			dir = "ASC"
		}
		field := "published_at"
		nulls := " NULLS LAST"
		switch q.SortField {
		case "words":
			field, nulls = "wc", ""
		case "type":
			field, nulls = "type", ""
		default: // date 及其他(含 views 兜底)
			field = "published_at"
		}
		order = `(sticky > 0) DESC, sticky ASC, ` + field + " " + dir + nulls + `, id DESC`
	}
	rows, err := db.Query(ctx, `SELECT id, slug, title, summary, status, author_id, cover_media_id,
		allow_comments, is_public, views, type, sticky, published_at, created_at, updated_at, author_name, cover_url, cover_w, cover_h
		FROM (SELECT DISTINCT ON (p.id) `+postCols+`, LENGTH(p.content_md) AS wc, COALESCE(m.url,'') AS cover_url,
			COALESCE(m.width,0) AS cover_w, COALESCE(m.height,0) AS cover_h`+base+clause+` ORDER BY p.id) t
		ORDER BY `+order+` LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Post{}
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &p.Summary, &p.Status, &p.AuthorID, &p.CoverMediaID,
			&p.AllowComments, &p.IsPublic, &p.Views, &p.Type, &p.Sticky, &p.PublishedAt, &p.CreatedAt, &p.UpdatedAt, &p.AuthorName, &p.CoverURL,
			&p.CoverWidth, &p.CoverHeight); err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	ids := make([]int64, len(out))
	for i := range out {
		ids[i] = out[i].ID
	}
	termsMap, err := db.termsForPosts(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	for i := range out {
		out[i].Terms = termsMap[out[i].ID]
	}
	return out, total, nil
}

// PostBySlug 返回含正文与 cover URL 的单篇文章。publicOnly 用于前端:未公开文章视为不存在。
func (db *DB) PostBySlug(ctx context.Context, slug string, publishedOnly bool, publicOnly bool) (*Post, error) {
	cond := `p.slug=$1`
	if publishedOnly {
		cond += ` AND p.status='published'`
	}
	if publicOnly {
		cond += ` AND p.is_public`
	}
	row := db.QueryRow(ctx, `SELECT p.id, p.slug, p.title, p.summary, p.status, p.author_id, p.cover_media_id,
		p.allow_comments, p.is_public, p.views, p.type, p.sticky, p.published_at, p.created_at, p.updated_at, u.display_name,
		p.content_md, p.content_html, COALESCE(m.url,''), COALESCE(m.width,0), COALESCE(m.height,0)
		FROM posts p JOIN users u ON u.id=p.author_id
		LEFT JOIN media m ON m.id=p.cover_media_id WHERE `+cond, slug)
	out := &Post{}
	if err := row.Scan(&out.ID, &out.Slug, &out.Title, &out.Summary, &out.Status, &out.AuthorID, &out.CoverMediaID,
		&out.AllowComments, &out.IsPublic, &out.Views, &out.Type, &out.Sticky, &out.PublishedAt, &out.CreatedAt, &out.UpdatedAt, &out.AuthorName,
		&out.ContentMD, &out.ContentHTML, &out.CoverURL, &out.CoverWidth, &out.CoverHeight); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	tm, err := db.termsForPosts(ctx, []int64{out.ID})
	if err != nil {
		return nil, err
	}
	out.Terms = tm[out.ID]
	return out, nil
}

func (db *DB) PostByID(ctx context.Context, id int64) (*Post, error) {
	return scanPost(db.QueryRow(ctx, `SELECT `+postCols+` FROM posts p JOIN users u ON u.id=p.author_id WHERE p.id=$1`, id))
}

type TermRef struct {
	Name string
	Slug string
}

type PostInput struct {
	Slug          string
	Title         string
	Summary       string
	ContentMD     string
	ContentHTML   string
	Status        string
	Type          string // standard / moment
	AuthorID      int64
	CoverMediaID  *int64
	AllowComments bool
	IsPublic      bool
	PublishedAt   *time.Time
	Categories    []TermRef
	Tags          []TermRef
}

func (db *DB) CreatePost(ctx context.Context, in PostInput) (int64, error) {
	if in.Type == "" {
		in.Type = "standard"
	}
	var id int64
	err := db.inTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO posts(slug,title,summary,content_md,content_html,status,type,author_id,cover_media_id,allow_comments,is_public,published_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`,
			in.Slug, in.Title, in.Summary, in.ContentMD, in.ContentHTML, in.Status, in.Type, in.AuthorID,
			in.CoverMediaID, in.AllowComments, in.IsPublic, in.PublishedAt).Scan(&id); err != nil {
			return err
		}
		return db.setPostTerms(ctx, tx, id, in.Categories, in.Tags)
	})
	return id, err
}

func (db *DB) UpdatePost(ctx context.Context, id int64, in PostInput) error {
	if in.Type == "" {
		in.Type = "standard"
	}
	return db.inTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE posts SET slug=$2,title=$3,summary=$4,content_md=$5,content_html=$6,status=$7,type=$8,
			cover_media_id=$9,allow_comments=$10,is_public=$11,published_at=COALESCE($12,published_at),updated_at=now() WHERE id=$1`,
			id, in.Slug, in.Title, in.Summary, in.ContentMD, in.ContentHTML, in.Status, in.Type,
			in.CoverMediaID, in.AllowComments, in.IsPublic, in.PublishedAt)
		if err != nil {
			return err
		}
		return db.setPostTerms(ctx, tx, id, in.Categories, in.Tags)
	})
}

func (db *DB) DeletePost(ctx context.Context, id int64) error {
	_, err := db.Exec(ctx, `DELETE FROM posts WHERE id=$1`, id)
	return err
}

func (db *DB) PublishPost(ctx context.Context, id int64) error {
	_, err := db.Exec(ctx, `UPDATE posts SET status='published', published_at=COALESCE(published_at, now()), updated_at=now() WHERE id=$1`, id)
	return err
}

// SetPostPublic 仅切换公开性,不动其他字段(列表页快捷开关用)。
func (db *DB) SetPostPublic(ctx context.Context, id int64, public bool) error {
	_, err := db.Exec(ctx, `UPDATE posts SET is_public=$2, updated_at=now() WHERE id=$1`, id, public)
	return err
}

// SetPostSticky 置顶(排到队尾)或取消置顶。
func (db *DB) SetPostSticky(ctx context.Context, id int64, on bool) error {
	if !on {
		_, err := db.Exec(ctx, `UPDATE posts SET sticky=0, updated_at=now() WHERE id=$1`, id)
		return err
	}
	_, err := db.Exec(ctx, `UPDATE posts SET sticky=(SELECT COALESCE(MAX(sticky),0)+1 FROM posts), updated_at=now() WHERE id=$1`, id)
	return err
}

// BulkPublishPosts 批量发布,单条 SQL 完成;返回实际更新行数。
func (db *DB) BulkPublishPosts(ctx context.Context, ids []int64) (int64, error) {
	tag, err := db.Exec(ctx, `UPDATE posts SET status='published', published_at=COALESCE(published_at, now()), updated_at=now() WHERE id = ANY($1)`, ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// BulkSetPostsPublic 批量切换公开性;返回实际更新行数。
func (db *DB) BulkSetPostsPublic(ctx context.Context, ids []int64, public bool) (int64, error) {
	tag, err := db.Exec(ctx, `UPDATE posts SET is_public=$2, updated_at=now() WHERE id = ANY($1)`, ids, public)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// BulkSetPostsSticky 批量置顶/取消置顶。置顶时接着现有最大权重按传入顺序往后排,
// 与单条 SetPostSticky 的「排到队尾」语义一致。
func (db *DB) BulkSetPostsSticky(ctx context.Context, ids []int64, on bool) (int64, error) {
	q := `WITH ord AS (SELECT id, ord::int AS rn FROM unnest($1::bigint[]) WITH ORDINALITY AS t(id, ord))
		UPDATE posts p SET sticky = b.base + o.rn, updated_at = now()
		FROM ord o CROSS JOIN (SELECT COALESCE(MAX(sticky),0) AS base FROM posts) b
		WHERE p.id = o.id`
	if !on {
		q = `UPDATE posts SET sticky=0, updated_at=now() WHERE id = ANY($1) AND sticky>0`
	}
	tag, err := db.Exec(ctx, q, ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// SetStickyOrder 按传入 id 顺序重写置顶权重(1..N);未包含的置顶文章取消置顶。
func (db *DB) SetStickyOrder(ctx context.Context, ids []int64) error {
	return db.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE posts SET sticky=0 WHERE sticky>0 AND NOT (id = ANY($1))`, ids); err != nil {
			return err
		}
		for i, id := range ids {
			if _, err := tx.Exec(ctx, `UPDATE posts SET sticky=$2, updated_at=now() WHERE id=$1`, id, i+1); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListStickyPosts 置顶文章(sticky 升序),管理端拖动排序用;authorID 非 0 时仅返回该作者自己的。
func (db *DB) ListStickyPosts(ctx context.Context, authorID int64) ([]Post, error) {
	q := `SELECT ` + postCols + ` FROM posts p JOIN users u ON u.id=p.author_id WHERE p.sticky>0`
	var args []any
	if authorID > 0 {
		q += ` AND p.author_id=$1`
		args = append(args, authorID)
	}
	q += ` ORDER BY p.sticky ASC`
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Post{}
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &p.Summary, &p.Status, &p.AuthorID, &p.CoverMediaID,
			&p.AllowComments, &p.IsPublic, &p.Views, &p.Type, &p.Sticky, &p.PublishedAt, &p.CreatedAt, &p.UpdatedAt, &p.AuthorName); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (db *DB) IncrViews(ctx context.Context, id int64) {
	_, _ = db.Exec(ctx, `UPDATE posts SET views=views+1 WHERE id=$1`, id)
}

// SitemapEntry sitemap.xml 所需的最小字段。
type SitemapEntry struct {
	Slug      string
	UpdatedAt time.Time
}

// SitemapPosts 已发布且公开的文章(仅 slug 与更新时间)。
func (db *DB) SitemapPosts(ctx context.Context) ([]SitemapEntry, error) {
	rows, err := db.Query(ctx, `SELECT slug, COALESCE(updated_at, published_at, created_at)
		FROM posts WHERE status='published' AND is_public ORDER BY published_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SitemapEntry, 0, 64)
	for rows.Next() {
		var e SitemapEntry
		if err := rows.Scan(&e.Slug, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (db *DB) setPostTerms(ctx context.Context, tx pgx.Tx, postID int64, cats, tags []TermRef) error {
	if _, err := tx.Exec(ctx, `DELETE FROM post_terms WHERE post_id=$1`, postID); err != nil {
		return err
	}
	for _, s := range cats {
		if err := attachTerm(ctx, tx, postID, s, "category"); err != nil {
			return err
		}
	}
	for _, s := range tags {
		if err := attachTerm(ctx, tx, postID, s, "tag"); err != nil {
			return err
		}
	}
	return nil
}

func attachTerm(ctx context.Context, tx pgx.Tx, postID int64, ref TermRef, typ string) error {
	if ref.Slug == "" {
		ref.Slug = ref.Name
	}
	var termID int64
	// 按 name 优先、slug 其次匹配,防止同名不同 slug 产生重复 term
	err := tx.QueryRow(ctx, `SELECT id FROM terms WHERE type=$1 AND (name=$2 OR slug=$3) ORDER BY (name=$2) DESC, id LIMIT 1`, typ, ref.Name, ref.Slug).Scan(&termID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO terms(name,slug,type) VALUES($1,$2,$3) RETURNING id`, ref.Name, ref.Slug, typ).Scan(&termID)
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO post_terms(post_id,term_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, postID, termID)
	return err
}

func (db *DB) termsForPosts(ctx context.Context, ids []int64) (map[int64][]Term, error) {
	out := make(map[int64][]Term, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := db.Query(ctx, `SELECT pt.post_id, t.id, t.name, t.slug, t.type
		FROM post_terms pt JOIN terms t ON t.id=pt.term_id WHERE pt.post_id = ANY($1) ORDER BY t.type, t.name`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pid int64
		var t Term
		if err := rows.Scan(&pid, &t.ID, &t.Name, &t.Slug, &t.Type); err != nil {
			return nil, err
		}
		out[pid] = append(out[pid], t)
	}
	return out, rows.Err()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
