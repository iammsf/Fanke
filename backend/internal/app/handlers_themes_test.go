package app

import (
	"archive/zip"
	"bytes"
	"testing"
)

func themeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestReadThemePackageTargets(t *testing.T) {
	data := themeZip(t, map[string]string{
		"info.json": `{"slug":"site-only","name":"站点主题","version":"1.0.0","targets":["site"]}`,
		"site.css":  "body{color:#000}",
	})
	info, files, err := readThemePackage(data, "pkg.zip")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Slug != "site-only" || len(info.Targets) != 1 || info.Targets[0] != "site" {
		t.Fatalf("unexpected info: %+v", info)
	}
	if _, ok := files["site.css"]; !ok {
		t.Fatal("site.css missing")
	}
}

func TestReadThemePackageLegacyTokens(t *testing.T) {
	data := themeZip(t, map[string]string{"tokens.css": ":root{--primary:#000}"})
	info, _, err := readThemePackage(data, "legacy.zip")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Slug != "legacy" || len(info.Targets) != 2 {
		t.Fatalf("legacy theme should target both, got %+v", info)
	}
}

func TestReadThemePackageRejectsMissingCSS(t *testing.T) {
	data := themeZip(t, map[string]string{
		"info.json": `{"slug":"bad","targets":["admin"]}`,
		"site.css":  "body{}",
	})
	if _, _, err := readThemePackage(data, "bad.zip"); err == nil {
		t.Fatal("expected error for missing admin.css")
	}
}
