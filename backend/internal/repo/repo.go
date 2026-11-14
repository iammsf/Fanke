// Package repo 提供 Postgres 连接池、迁移执行与各实体的仓储层。
package repo

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fanke/backend/internal/secret"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type DB struct {
	*pgxpool.Pool
	// Box 敏感字段字段级加密;nil 表示未启用(未配置主密钥的开发环境)。
	Box *secret.Box
}

// seal 加密敏感字段;加密失败或无密钥时退回明文,避免因密钥问题导致业务不可用。
func (db *DB) seal(v string) string {
	if db == nil || db.Box == nil || v == "" {
		return v
	}
	out, err := db.Box.Seal(v)
	if err != nil {
		return v
	}
	return out
}

// unseal 解密敏感字段;非密文或解密失败时原样返回(兼容历史明文)。
func (db *DB) unseal(v string) string {
	if db == nil || db.Box == nil || v == "" {
		return v
	}
	out, err := db.Box.Open(v)
	if err != nil {
		return v
	}
	return out
}

func Open(ctx context.Context, url string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 8
	cfg.MaxConnLifetime = 30 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &DB{Pool: pool}, nil
}

// Migrate 按文件名顺序执行未应用的迁移。
func (db *DB) Migrate(ctx context.Context) error {
	if _, err := db.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		var exists bool
		if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		err = db.inTx(ctx, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, name)
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

func (db *DB) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
