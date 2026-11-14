package repo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Plugin struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CSS       string    `json:"css"`
	JS        string    `json:"js"`
	Enabled   bool      `json:"enabled"`
	Sort      int       `json:"sort"`
	CreatedAt time.Time `json:"created_at"`
}

func (db *DB) ListPlugins(ctx context.Context, enabledOnly bool) ([]Plugin, error) {
	q := `SELECT id,name,css,js,enabled,sort,created_at FROM plugins`
	if enabledOnly {
		q += ` WHERE enabled=TRUE`
	}
	q += ` ORDER BY sort, id`
	rows, err := db.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Plugin{}
	for rows.Next() {
		var p Plugin
		if err := rows.Scan(&p.ID, &p.Name, &p.CSS, &p.JS, &p.Enabled, &p.Sort, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (db *DB) CreatePlugin(ctx context.Context, p Plugin) (int64, error) {
	var id int64
	err := db.QueryRow(ctx, `INSERT INTO plugins(name,css,js,enabled,sort) VALUES($1,$2,$3,$4,$5) RETURNING id`,
		p.Name, p.CSS, p.JS, p.Enabled, p.Sort).Scan(&id)
	return id, err
}

func (db *DB) UpdatePlugin(ctx context.Context, id int64, p Plugin) error {
	_, err := db.Exec(ctx, `UPDATE plugins SET name=$2,css=$3,js=$4,enabled=$5,sort=$6 WHERE id=$1`,
		id, p.Name, p.CSS, p.JS, p.Enabled, p.Sort)
	return err
}

func (db *DB) DeletePlugin(ctx context.Context, id int64) error {
	_, err := db.Exec(ctx, `DELETE FROM plugins WHERE id=$1`, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
