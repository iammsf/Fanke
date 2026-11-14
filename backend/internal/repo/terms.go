package repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type TermWithCount struct {
	Term
	Count int `json:"count"`
}

func (db *DB) ListTerms(ctx context.Context, typ string) ([]TermWithCount, error) {
	rows, err := db.Query(ctx, `SELECT t.id, t.name, t.slug, t.type, count(pt.post_id)::int
		FROM terms t LEFT JOIN post_terms pt ON pt.term_id=t.id
		WHERE ($1='' OR t.type=$1)
		GROUP BY t.id ORDER BY t.name`, typ)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TermWithCount{}
	for rows.Next() {
		var t TermWithCount
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.Type, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (db *DB) CreateTerm(ctx context.Context, name, slug, typ string) (int64, error) {
	var id int64
	err := db.QueryRow(ctx, `INSERT INTO terms(name,slug,type) VALUES($1,$2,$3) RETURNING id`, name, slug, typ).Scan(&id)
	return id, err
}

func (db *DB) UpdateTerm(ctx context.Context, id int64, name, slug string) error {
	_, err := db.Exec(ctx, `UPDATE terms SET name=$2, slug=$3 WHERE id=$1`, id, name, slug)
	return err
}

func (db *DB) DeleteTerm(ctx context.Context, id int64) error {
	_, err := db.Exec(ctx, `DELETE FROM terms WHERE id=$1`, id)
	return err
}

// EnsureTerm 幂等取/建 term,返回 id(迁移与后台保存共用)。
func (db *DB) EnsureTerm(ctx context.Context, name, slug, typ string) (int64, error) {
	var id int64
	err := db.QueryRow(ctx, `SELECT id FROM terms WHERE slug=$1 AND type=$2`, slug, typ).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	return db.CreateTerm(ctx, name, slug, typ)
}
