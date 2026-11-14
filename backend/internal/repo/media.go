package repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (db *DB) ListMedia(ctx context.Context, limit, offset int) ([]Media, int, error) {
	var total int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM media`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := db.Query(ctx, `SELECT id, filename, storage, key, url, mime, size, width, height, sha256, created_at
		FROM media ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Media{}
	for rows.Next() {
		var m Media
		if err := rows.Scan(&m.ID, &m.Filename, &m.Storage, &m.Key, &m.URL, &m.MIME, &m.Size,
			&m.Width, &m.Height, &m.SHA256, &m.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

// MediaBySHA 按内容哈希查重(迁移与上传共用)。
func (db *DB) MediaBySHA(ctx context.Context, sha string) (*Media, error) {
	var m Media
	err := db.QueryRow(ctx, `SELECT id, filename, storage, key, url, mime, size, width, height, sha256, created_at
		FROM media WHERE sha256=$1 ORDER BY id LIMIT 1`, sha).Scan(&m.ID, &m.Filename, &m.Storage, &m.Key, &m.URL,
		&m.MIME, &m.Size, &m.Width, &m.Height, &m.SHA256, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// HasLocalMedia 指定哈希是否已有 local 记录(本地副本导入去重用)。
func (db *DB) HasLocalMedia(ctx context.Context, sha string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM media WHERE sha256=$1 AND storage='local')`, sha).Scan(&exists)
	return exists, err
}

func (db *DB) CreateMedia(ctx context.Context, m Media) (int64, error) {
	var id int64
	err := db.QueryRow(ctx, `INSERT INTO media(filename,storage,key,url,mime,size,width,height,sha256)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		m.Filename, m.Storage, m.Key, m.URL, m.MIME, m.Size, m.Width, m.Height, m.SHA256).Scan(&id)
	return id, err
}

// FetchMedia 返回带 created_at 的完整记录。
func (db *DB) FetchMedia(ctx context.Context, id int64) (*Media, error) { return db.MediaByID(ctx, id) }

func (db *DB) DeleteMedia(ctx context.Context, id int64) (*Media, error) {
	var m Media
	err := db.QueryRow(ctx, `DELETE FROM media WHERE id=$1
		RETURNING id, filename, storage, key, url, mime, size, width, height, sha256, created_at`, id).
		Scan(&m.ID, &m.Filename, &m.Storage, &m.Key, &m.URL, &m.MIME, &m.Size, &m.Width, &m.Height, &m.SHA256, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &m, err
}

func (db *DB) MediaByID(ctx context.Context, id int64) (*Media, error) {
	var m Media
	err := db.QueryRow(ctx, `SELECT id, filename, storage, key, url, mime, size, width, height, sha256, created_at
		FROM media WHERE id=$1`, id).Scan(&m.ID, &m.Filename, &m.Storage, &m.Key, &m.URL,
		&m.MIME, &m.Size, &m.Width, &m.Height, &m.SHA256, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &m, err
}

// UpdateMediaStorage 批量转移存储后更新 storage/key/url。
func (db *DB) UpdateMediaStorage(ctx context.Context, id int64, storage, key, url string) error {
	_, err := db.Exec(ctx, `UPDATE media SET storage=$2, key=$3, url=$4 WHERE id=$1`, id, storage, key, url)
	return err
}

// ReplaceMediaURL 把文章正文(content_md/content_html)中引用的旧媒体 URL 统一替换为新 URL,返回受影响文章数。
// 封面无需处理:cover_url 由 cover_media_id JOIN media.url 动态得出,随 media 行更新。
func (db *DB) ReplaceMediaURL(ctx context.Context, oldURL, newURL string) (int64, error) {
	if oldURL == "" || oldURL == newURL {
		return 0, nil
	}
	ct, err := db.Exec(ctx, `UPDATE posts SET content_md=replace(content_md,$1,$2), content_html=replace(content_html,$1,$2)
		WHERE content_md LIKE '%'||$1||'%' OR content_html LIKE '%'||$1||'%'`, oldURL, newURL)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

// AllMedia 返回全部媒体记录(媒体库同步对账用,不分页;避免翻页过程中数据变动导致漏项)。
func (db *DB) AllMedia(ctx context.Context) ([]Media, error) {
	rows, err := db.Query(ctx, `SELECT id, filename, storage, key, url, mime, size, width, height, sha256, created_at
		FROM media ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Media{}
	for rows.Next() {
		var m Media
		if err := rows.Scan(&m.ID, &m.Filename, &m.Storage, &m.Key, &m.URL, &m.MIME, &m.Size,
			&m.Width, &m.Height, &m.SHA256, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MediaRefPosts 统计引用该媒体的文章数(封面 cover_media_id + 正文内容里的 URL),用于删除记录前的风险提示。
// 用 position() 而不是 LIKE,避免 URL 里的 _ / % 被当通配符。
func (db *DB) MediaRefPosts(ctx context.Context, id int64, url string) (int, error) {
	var n int
	err := db.QueryRow(ctx, `SELECT count(*) FROM posts
		WHERE cover_media_id=$1 OR position($2 in content_md)>0 OR position($2 in content_html)>0`, id, url).Scan(&n)
	return n, err
}

// MediaDimsByURLs 批量取一组 URL 的原始宽高(正文内嵌图注入 width/height/srcset 用)。
// 未登记或尺寸未知的 URL 不会出现在结果里,调用方按"拿不到就不加属性"处理。
func (db *DB) MediaDimsByURLs(ctx context.Context, urls []string) (map[string][2]int, error) {
	out := map[string][2]int{}
	if len(urls) == 0 {
		return out, nil
	}
	rows, err := db.Query(ctx, `SELECT url, width, height FROM media
		WHERE url = ANY($1) AND width > 0 AND height > 0`, urls)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var u string
		var w, h int
		if err := rows.Scan(&u, &w, &h); err != nil {
			return out, err
		}
		out[u] = [2]int{w, h}
	}
	return out, rows.Err()
}

// MediaKeyExists 指定 key 是否已有记录(同步入库前查重,规避 UNIQUE(storage,key) 冲突)。
func (db *DB) MediaKeyExists(ctx context.Context, key string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM media WHERE key=$1)`, key).Scan(&exists)
	return exists, err
}

// MediaURLRefPosts 按完整 URL 统计被文章引用的篇数(桶内孤儿入库/删除前的风险提示:
// 手工传到 S3 的文件可能被正文直接 <img>/<video> 引用,删桶对象前必须提示)。
func (db *DB) MediaURLRefPosts(ctx context.Context, url string) (int, error) {
	var n int
	err := db.QueryRow(ctx, `SELECT count(*) FROM posts
		WHERE position($1 in content_md)>0 OR position($1 in content_html)>0`, url).Scan(&n)
	return n, err
}
