package media

import (
	"context"
	"fmt"
	"io"

	"fanke/backend/internal/pluginrpc"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

const ImmutableCacheControl = "public, max-age=31536000, immutable"

type S3 struct {
	cfg    S3Config
	runner pluginrpc.Runner
}

var pluginRootBase struct {
	sync.RWMutex
	dir string
}

func ConfigurePluginRoot(dir string) {
	pluginRootBase.Lock()
	pluginRootBase.dir = dir
	pluginRootBase.Unlock()
}

func pluginRootDir(name string) string {
	pluginRootBase.RLock()
	dir := pluginRootBase.dir
	pluginRootBase.RUnlock()
	if dir == "" {
		dir = os.Getenv("EXTENSIONS_DIR")
	}
	return filepath.Join(dir, name)
}

func runtimeGOOSArch() string { return runtime.GOOS + "-" + runtime.GOARCH }
func newS3Runner() pluginrpc.Runner {
	root := os.Getenv("FAKE_S3_PLUGIN_DIR")
	if root == "" {
		root = pluginRootDir("s3-storage")
	}
	entry := os.Getenv("FAKE_S3_PLUGIN_ENTRYPOINT")
	if entry == "" {
		entry = filepath.Join("server", "bin", runtimeGOOSArch(), "s3-storage-server")
		if runtime.GOOS == "windows" {
			entry += ".exe"
		}
	}
	return pluginrpc.Runner{Root: root, Entrypoint: entry}
}
func (s *S3) Name() string { return "s3" }
func (s *S3) Save(ctx context.Context, key string, data io.Reader, size int64, mime string) (Object, error) {
	f, err := os.CreateTemp("", "fanke-s3-save-*")
	if err != nil {
		return Object{}, err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = io.Copy(f, data); err != nil {
		f.Close()
		return Object{}, err
	}
	if err = f.Close(); err != nil {
		return Object{}, err
	}
	var out Object
	err = s.runner.Call(ctx, "storage.save", map[string]any{"endpoint": s.cfg.Endpoint, "region": s.cfg.Region, "access_key": s.cfg.AccessKey, "secret_key": s.cfg.SecretKey, "session_token": s.cfg.SessionToken, "bucket": s.cfg.Bucket, "public_base": s.cfg.PublicBase, "force_path_style": s.cfg.ForcePathStyle, "key": key, "path": name, "mime": mime}, &out)
	return out, err
}
func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	f, err := os.CreateTemp("", "fanke-s3-open-*")
	if err != nil {
		return nil, err
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	var out struct {
		Path string `json:"path"`
	}
	err = s.runner.Call(ctx, "storage.open", map[string]any{"endpoint": s.cfg.Endpoint, "region": s.cfg.Region, "access_key": s.cfg.AccessKey, "secret_key": s.cfg.SecretKey, "session_token": s.cfg.SessionToken, "bucket": s.cfg.Bucket, "force_path_style": s.cfg.ForcePathStyle, "key": key, "path": path}, &out)
	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	if out.Path == "" {
		_ = os.Remove(path)
		return nil, fmt.Errorf("plugin returned empty path")
	}
	f, err = os.Open(out.Path)
	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return &tempReadCloser{File: f, path: path}, nil
}

type tempReadCloser struct {
	*os.File
	path string
}

func (f *tempReadCloser) Close() error {
	err := f.File.Close()
	if removeErr := os.Remove(f.path); err == nil {
		err = removeErr
	}
	return err
}
func (s *S3) Delete(ctx context.Context, key string) error {
	var out any
	return s.runner.Call(ctx, "storage.delete", map[string]any{"endpoint": s.cfg.Endpoint, "region": s.cfg.Region, "access_key": s.cfg.AccessKey, "secret_key": s.cfg.SecretKey, "session_token": s.cfg.SessionToken, "bucket": s.cfg.Bucket, "force_path_style": s.cfg.ForcePathStyle, "key": key}, &out)
}
