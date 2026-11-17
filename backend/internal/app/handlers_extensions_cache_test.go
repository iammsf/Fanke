package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"fanke/backend/internal/config"
)

func newExtFileServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "image-viewer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "editor.js"), []byte("/* v1.5 */"), 0o644); err != nil {
		t.Fatal(err)
	}
	// URL 里的 assets/ 是虚拟前缀,映射到插件根目录。
	if err := os.WriteFile(filepath.Join(dir, "logo.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &App{Cfg: &config.Config{ExtensionsDir: root}}
	r := chi.NewRouter()
	r.Get("/ext/{slug}/page/*", a.ServeExtensionPageFile)
	r.Get("/ext/{slug}/assets/*", a.ServeExtensionAsset)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, dir
}

func TestExtensionCodeFilesAreNotCachedByBrowser(t *testing.T) {
	srv, dir := newExtFileServer(t)

	res, err := http.Get(srv.URL + "/ext/image-viewer/page/editor.js")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if got := res.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
	lastMod := res.Header.Get("Last-Modified")
	if lastMod == "" {
		t.Fatal("missing Last-Modified, conditional requests cannot revalidate")
	}

	// 插件更新后 editor.js 内容与 mtime 都变了:带 If-Modified-Since 回源必须拿到新字节。
	if err := os.WriteFile(filepath.Join(dir, "editor.js"), []byte("/* v1.6 */"), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(dir, "editor.js"), fresh, fresh); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("GET", srv.URL+"/ext/image-viewer/page/editor.js", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("If-Modified-Since", lastMod)
	res2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("after update status = %d, want 200 (304 would keep the stale script)", res2.StatusCode)
	}
	body, err := io.ReadAll(res2.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "/* v1.6 */" {
		t.Fatalf("body = %q, want the updated script", body)
	}
}

func TestExtensionBinaryAssetsKeepLongCache(t *testing.T) {
	srv, _ := newExtFileServer(t)
	res, err := http.Get(srv.URL + "/ext/image-viewer/assets/logo.png")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if got := res.Header.Get("Cache-Control"); got != "public, max-age=3600" {
		t.Fatalf("Cache-Control = %q, want public, max-age=3600", got)
	}
}
