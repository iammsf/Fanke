// Package media 提供本地磁盘与 S3 兼容对象存储 两种媒体存储驱动。
package media

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Object struct {
	Key  string
	URL  string
	MIME string
	Size int64
}

type Storage interface {
	// Save 写入 data(size 字节)到 key,返回可访问 URL。
	Save(ctx context.Context, key string, data io.Reader, size int64, mime string) (Object, error)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Name() string
}

// ---------- local ----------

type Local struct {
	dir     string
	baseURL string
}

func NewLocal(dir, baseURL string) (*Local, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Local{dir: dir, baseURL: strings.TrimRight(baseURL, "/")}, nil
}

func (l *Local) Name() string { return "local" }

func (l *Local) path(key string) (string, error) {
	if key == "" || strings.ContainsAny(key, "\\:\x00") || strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("invalid key")
	}
	for _, part := range strings.Split(key, "/") {
		if part == ".." || part == "." || part == "" {
			return "", fmt.Errorf("invalid key")
		}
	}
	p := filepath.Join(l.dir, filepath.FromSlash(key))
	if !strings.HasPrefix(p, filepath.Clean(l.dir)+string(os.PathSeparator)) && p != filepath.Clean(l.dir) {
		return "", fmt.Errorf("invalid key")
	}
	return p, nil
}

func (l *Local) Save(ctx context.Context, key string, data io.Reader, size int64, mime string) (Object, error) {
	p, err := l.path(key)
	if err != nil {
		return Object{}, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return Object{}, err
	}
	f, err := os.Create(p)
	if err != nil {
		return Object{}, err
	}
	defer f.Close()
	n, err := io.Copy(f, data)
	if err != nil {
		return Object{}, err
	}
	return Object{Key: key, URL: l.baseURL + "/" + key, MIME: mime, Size: n}, nil
}

func (l *Local) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (l *Local) Delete(ctx context.Context, key string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	return os.Remove(p)
}
