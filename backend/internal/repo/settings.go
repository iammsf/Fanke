package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"fanke/backend/internal/media"
)

// S3Config 读取迁移后的对象存储配置。
type S3Settings struct {
	Buckets         []media.S3Config `json:"buckets"`
	DefaultBucketID string           `json:"default_bucket_id"`
}

func StableBucketID(c media.S3Config) string {
	if c.ID != "" {
		return c.ID
	}
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s", c.Endpoint, c.Region, c.Bucket, c.PublicBase)))
	return "s3-" + hex.EncodeToString(h[:8])
}

func MigrateS3Config(c media.S3Config) S3Settings {
	if c.Bucket == "" {
		return S3Settings{}
	}
	c.ID = StableBucketID(c)
	if c.Alias == "" {
		c.Alias = c.Bucket
	}
	return S3Settings{Buckets: []media.S3Config{c}, DefaultBucketID: c.ID}
}

func (s S3Settings) SelectBucket(id string) (media.S3Config, error) {
	if id == "" {
		id = s.DefaultBucketID
		if id == "" && len(s.Buckets) > 0 {
			return s.Buckets[0], nil
		}
	}
	for _, c := range s.Buckets {
		if c.ID == id {
			return c, nil
		}
	}
	return media.S3Config{}, fmt.Errorf("unknown S3 bucket")
}

func (db *DB) S3Settings(ctx context.Context) (S3Settings, error) {
	var out S3Settings
	raw, err := db.GetSetting(ctx, "s3_buckets")
	if errors.Is(err, pgx.ErrNoRows) {
		legacyRaw, e := db.GetSetting(ctx, "s3_config")
		if errors.Is(e, pgx.ErrNoRows) {
			return out, nil
		}
		if e != nil {
			return out, e
		}
		var legacy media.S3Config
		if e = json.Unmarshal(legacyRaw, &legacy); e != nil {
			return out, e
		}
		legacy = db.unsealBucket(legacy)
		out = MigrateS3Config(legacy)
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	for i := range out.Buckets {
		out.Buckets[i] = db.unsealBucket(out.Buckets[i])
		out.Buckets[i].ID = StableBucketID(out.Buckets[i])
		if out.Buckets[i].Alias == "" {
			out.Buckets[i].Alias = out.Buckets[i].Bucket
		}
	}
	return out, nil
}

func (db *DB) PutS3Settings(ctx context.Context, s S3Settings) error {
	for i := range s.Buckets {
		s.Buckets[i].ID = StableBucketID(s.Buckets[i])
		s.Buckets[i] = db.sealBucket(s.Buckets[i])
	}
	return db.PutSetting(ctx, "s3_buckets", s)
}

// 敏感字段(AccessKey/SecretKey/SessionToken)落库前加密、读取后解密。
func (db *DB) sealBucket(c media.S3Config) media.S3Config {
	c.AccessKey = db.seal(c.AccessKey)
	c.SecretKey = db.seal(c.SecretKey)
	c.SessionToken = db.seal(c.SessionToken)
	return c
}

func (db *DB) unsealBucket(c media.S3Config) media.S3Config {
	c.AccessKey = db.unseal(c.AccessKey)
	c.SecretKey = db.unseal(c.SecretKey)
	c.SessionToken = db.unseal(c.SessionToken)
	return c
}

func (db *DB) S3Config(ctx context.Context) (media.S3Config, error) {
	s, err := db.S3Settings(ctx)
	if err != nil {
		return media.S3Config{}, err
	}
	for _, c := range s.Buckets {
		if c.ID == s.DefaultBucketID {
			return c, nil
		}
	}
	if len(s.Buckets) > 0 {
		return s.Buckets[0], nil
	}
	return media.S3Config{}, nil
}

func (db *DB) PutS3Config(ctx context.Context, c media.S3Config) error {
	c.ID = StableBucketID(c)
	return db.PutS3Settings(ctx, S3Settings{Buckets: []media.S3Config{c}, DefaultBucketID: c.ID})
}

func (db *DB) GetSetting(ctx context.Context, key string) (json.RawMessage, error) {
	var raw []byte
	err := db.QueryRow(ctx, `SELECT value FROM settings WHERE key=$1`, key).Scan(&raw)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func (db *DB) PutSetting(ctx context.Context, key string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `INSERT INTO settings(key,value) VALUES($1,$2)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, key, b)
	return err
}

func (db *DB) AllSettings(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := db.Query(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = json.RawMessage(v)
	}
	return out, rows.Err()
}
