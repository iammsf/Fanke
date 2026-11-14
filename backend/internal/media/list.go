package media

import (
	"context"
	"time"
)

type ObjectMeta struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
	ETag         string    `json:"etag"`
}

func ListObjects(ctx context.Context, c S3Config, max int) ([]ObjectMeta, bool, error) {
	var out struct {
		Objects   []ObjectMeta `json:"objects"`
		Truncated bool         `json:"truncated"`
	}
	err := newS3Runner().Call(ctx, "storage.list", map[string]any{"config": c, "max": max}, &out)
	return out.Objects, out.Truncated, err
}
func StatObject(ctx context.Context, c S3Config, key string) (int64, string, error) {
	var out struct {
		Size int64  `json:"size"`
		MIME string `json:"mime"`
	}
	err := newS3Runner().Call(ctx, "storage.head", map[string]any{"config": c, "key": key}, &out)
	return out.Size, out.MIME, err
}
func ObjectCacheControl(ctx context.Context, c S3Config, key string) (string, error) {
	var out struct {
		CacheControl string `json:"cache_control"`
	}
	err := newS3Runner().Call(ctx, "storage.head", map[string]any{"config": c, "key": key}, &out)
	return out.CacheControl, err
}
func SetCacheControl(ctx context.Context, c S3Config, key, value string) error {
	return newS3Runner().Call(ctx, "storage.setCacheControl", map[string]any{"config": c, "key": key, "cache_control": value}, nil)
}
