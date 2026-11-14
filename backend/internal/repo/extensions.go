package repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Extension 已安装插件的元数据(对应 extensions_data 表)。
type Extension struct {
	ID          int64      `json:"id"`
	Slug        string     `json:"slug"`
	Name        string     `json:"name"`
	Author      string     `json:"author"`
	Version     string     `json:"version"`
	Date        string     `json:"date"`
	Description string     `json:"description"`
	URL         string     `json:"url"`
	UpdateURL   string     `json:"update_url"`
	Favico      string     `json:"favico"`
	Hooks       []string   `json:"hooks"`
	Enabled     bool       `json:"enabled"`
	Initialized bool       `json:"initialized"`
	InstalledAt time.Time  `json:"installed_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Latest      *ExtUpdate `json:"latest,omitempty"` // check-update 时填充
}

// ExtUpdate check-update 结果(不入库,响应时临时挂载)。
type ExtUpdate struct {
	Version     string `json:"version"`
	DownloadURL string `json:"download_url"`
	Date        string `json:"date"`
	HasUpdate   bool   `json:"has_update"`
}

func scanExt(row pgx.Row) (*Extension, error) {
	var e Extension
	var hooks []byte
	err := row.Scan(&e.ID, &e.Slug, &e.Name, &e.Author, &e.Version, &e.Date, &e.Description,
		&e.URL, &e.UpdateURL, &e.Favico, &hooks, &e.Enabled, &e.Initialized, &e.InstalledAt, &e.UpdatedAt)
	if err != nil {
		return nil, err
	}
	e.Hooks = []string{}
	_ = json.Unmarshal(hooks, &e.Hooks)
	return &e, nil
}

const extCols = `id, slug, name, author, version, date, description, url, update_url, favico, hooks, enabled, initialized, installed_at, updated_at`

func (db *DB) ListExtensions(ctx context.Context) ([]Extension, error) {
	rows, err := db.Query(ctx, `SELECT `+extCols+` FROM extensions_data ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Extension{}
	for rows.Next() {
		e, err := scanExt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (db *DB) ExtensionBySlug(ctx context.Context, slug string) (*Extension, error) {
	e, err := scanExt(db.QueryRow(ctx, `SELECT `+extCols+` FROM extensions_data WHERE slug=$1`, slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

func (db *DB) ExtensionByID(ctx context.Context, id int64) (*Extension, error) {
	e, err := scanExt(db.QueryRow(ctx, `SELECT `+extCols+` FROM extensions_data WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

func (db *DB) CreateExtension(ctx context.Context, e Extension) (int64, error) {
	hooks, _ := json.Marshal(e.Hooks)
	var id int64
	err := db.QueryRow(ctx, `INSERT INTO extensions_data
		(slug, name, author, version, date, description, url, update_url, favico, hooks)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		e.Slug, e.Name, e.Author, e.Version, e.Date, e.Description, e.URL, e.UpdateURL, e.Favico, hooks).Scan(&id)
	return id, err
}

func (db *DB) SetExtensionEnabled(ctx context.Context, id int64, enabled bool) error {
	_, err := db.Exec(ctx, `UPDATE extensions_data SET enabled=$2, updated_at=now() WHERE id=$1`, id, enabled)
	return err
}

func (db *DB) SetExtensionInitialized(ctx context.Context, id int64) error {
	_, err := db.Exec(ctx, `UPDATE extensions_data SET initialized=TRUE, updated_at=now() WHERE id=$1`, id)
	return err
}

func (db *DB) DeleteExtension(ctx context.Context, id int64) error {
	_, err := db.Exec(ctx, `DELETE FROM extensions_data WHERE id=$1`, id)
	return err
}

// ReplaceMediaURLInExtensions 同步替换所有插件数据表(ext_data_*)里引用的旧媒体 URL:
// k 命中旧 URL 的改写为新 URL(新 k 已存在则跳过,避免唯一键冲突);v 文本内的旧 URL 一并替换。
// 返回受影响行数(k 改写 + v 替换合计)。
func (db *DB) ReplaceMediaURLInExtensions(ctx context.Context, oldURL, newURL string) (int64, error) {
	if oldURL == "" || oldURL == newURL {
		return 0, nil
	}
	rows, err := db.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename LIKE 'ext\_data\_%'`)
	if err != nil {
		return 0, err
	}
	var tables []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return 0, err
		}
		tables = append(tables, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	var total int64
	for _, t := range tables {
		q := pgx.Identifier{t}.Sanitize()
		ct, err := db.Exec(ctx, `UPDATE `+q+` SET k=$2 WHERE k=$1 AND NOT EXISTS (SELECT 1 FROM `+q+` x WHERE x.k=$2)`, oldURL, newURL)
		if err != nil {
			return total, err
		}
		total += ct.RowsAffected()
		ct, err = db.Exec(ctx, `UPDATE `+q+` SET v=replace(v::text,$1,$2)::jsonb WHERE v::text LIKE '%'||$1||'%'`, oldURL, newURL)
		if err != nil {
			return total, err
		}
		total += ct.RowsAffected()
	}
	return total, nil
}
