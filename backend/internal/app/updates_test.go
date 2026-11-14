package app

import "testing"

func TestVersionGreater(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"1.1.0", "1.0.0", true},
		{"1.0.1", "1.0.0", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "1.1.0", false},
		{"v2.0", "1.9.9", true},
		{"1.2.0-rc1", "1.1.0", true},
		{"2026.11.02", "2026.10.19", true},
		{"dev", "1.0.0", false},
		{"1.0.0", "dev", false},
		{"", "1.0.0", false},
	}
	for _, c := range cases {
		if got := versionGreater(c.latest, c.current); got != c.want {
			t.Errorf("versionGreater(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestDecodeExtUpdateManifest(t *testing.T) {
	data := []byte(`{"core":{"version":"1.0.0"},"plugins":{"image-viewer":{"version":"1.6","date":"2026.11.20","notes":"修复","page_url":"https://example.com","download_url":"https://example.com/a.zip"}}}`)
	got, err := decodeExtUpdate("image-viewer", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Version != "1.6" || got.PageURL != "https://example.com" || got.Notes != "修复" {
		t.Fatalf("unexpected entry: %+v", got)
	}
	if _, err := decodeExtUpdate("not-there", data); err == nil {
		t.Fatal("expected error for missing plugin")
	}
}

func TestDecodeExtUpdateFlat(t *testing.T) {
	got, err := decodeExtUpdate("image-focus", []byte(`{"version":"2.0","date":"2026.12.01","download_url":"https://example.com/b.zip"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Version != "2.0" || got.DownloadURL != "https://example.com/b.zip" {
		t.Fatalf("unexpected entry: %+v", got)
	}
	if _, err := decodeExtUpdate("x", []byte(`{"foo":1}`)); err == nil {
		t.Fatal("expected error for invalid body")
	}
}
