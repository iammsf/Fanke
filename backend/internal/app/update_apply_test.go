package app

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func writeTarGz(t *testing.T, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pkg.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractCoreArchive(t *testing.T) {
	pkg := writeTarGz(t, map[string]string{
		"fanke-v1.0.1-linux-amd64/bin/api":        "binary",
		"fanke-v1.0.1-linux-amd64/web/index.html": "<html></html>",
		"fanke-v1.0.1-linux-amd64/README.md":      "ignored",
	})
	dest := t.TempDir()
	if err := extractCoreArchive(pkg, dest); err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(dest, "bin", "api")); err != nil || string(b) != "binary" {
		t.Fatalf("bin/api missing or wrong: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "web", "index.html")); err != nil {
		t.Fatalf("web/index.html missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err == nil {
		t.Fatal("non-whitelisted file should be ignored")
	}
}

func TestExtractCoreArchiveRejectsTraversal(t *testing.T) {
	pkg := writeTarGz(t, map[string]string{
		"fanke/bin/api":        "binary",
		"fanke/web/index.html": "<html></html>",
		"fanke/web/../../evil": "oops",
	})
	if err := extractCoreArchive(pkg, t.TempDir()); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
}

// Nuxt 的 [...slug] 路由会产出 "_...xxxxxxxx.css" 这类文件名,不能被当成路径穿越拒绝。
func TestExtractCoreArchiveAllowsDotsInFileName(t *testing.T) {
	pkg := writeTarGz(t, map[string]string{
		"fanke/bin/api":                    "binary",
		"fanke/web/index.html":             "<html></html>",
		"fanke/web/_nuxt/_...WH8Ix9aR.css": "body{}",
	})
	dest := t.TempDir()
	if err := extractCoreArchive(pkg, dest); err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "web", "_nuxt", "_...WH8Ix9aR.css")); err != nil {
		t.Fatalf("dotted filename missing: %v", err)
	}
}
