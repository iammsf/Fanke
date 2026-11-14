package repo

import (
	"context"
	"github.com/jackc/pgx/v5"
)

type MediaVariant struct {
	MediaID           int64
	Width, Height     int
	Storage, Key, URL string
	Size              int64
}

func (db *DB) MediaVariants(ctx context.Context, id int64) ([]MediaVariant, error) {
	rows, err := db.Query(ctx, `SELECT media_id,width,height,storage,key,url,size FROM media_variants WHERE ($1::bigint=0 OR media_id=$1) ORDER BY width`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MediaVariant{}
	for rows.Next() {
		var v MediaVariant
		if err := rows.Scan(&v.MediaID, &v.Width, &v.Height, &v.Storage, &v.Key, &v.URL, &v.Size); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (db *DB) CreateMediaVariant(ctx context.Context, v MediaVariant) error {
	_, err := db.Exec(ctx, `INSERT INTO media_variants(media_id,width,height,storage,key,url,size) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (media_id,width) DO NOTHING`, v.MediaID, v.Width, v.Height, v.Storage, v.Key, v.URL, v.Size)
	return err
}

func (db *DB) MediaByURL(ctx context.Context, url string) (*Media, error) {
	var id int64
	err := db.QueryRow(ctx, `SELECT id FROM media WHERE url=$1 ORDER BY id LIMIT 1`, url).Scan(&id)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return db.MediaByID(ctx, id)
}

func (db *DB) TransferMediaFamily(ctx context.Context, id int64, storage, key, url string, variants []MediaVariant) error {
	return db.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE media SET storage=$2,key=$3,url=$4 WHERE id=$1`, id, storage, key, url); err != nil {
			return err
		}
		for _, v := range variants {
			if _, err := tx.Exec(ctx, `UPDATE media_variants SET storage=$4,url=$3 WHERE media_id=$1 AND width=$2`, id, v.Width, v.URL, storage); err != nil {
				return err
			}
		}
		return nil
	})
}
