package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/png"
	"io"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"
)

func TestVariantWidths(t *testing.T) {
	for _, tc := range []struct {
		in   int
		want []int
	}{{0, nil}, {200, []int{200}}, {480, []int{480}}, {600, []int{480, 600}}, {740, []int{480, 740}}, {900, []int{480, 740, 900}}, {1080, []int{480, 740, 1080}}, {2000, []int{480, 740, 1080}}} {
		if got := VariantWidths(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%d: %v != %v", tc.in, got, tc.want)
		}
	}
}

func TestVariantKey(t *testing.T) {
	for _, name := range []string{"旅行风景.jpg", "../../photo one.png", `C:\fakepath\photo.png`, "..", strings.Repeat("界", 100) + ".png"} {
		key := VariantKey(name, "abc123", 42, 740)
		if !strings.HasPrefix(key, VariantPrefix+"abc123/42/") || path.Clean(key) != key || strings.ContainsAny(key, "\\:\x00") || !strings.HasSuffix(key, "-740w.webp") {
			t.Fatalf("unsafe %q", key)
		}
	}
	if got := VariantKey("original-photo.jpg", "sha", 1, 480); !strings.HasSuffix(got, "/original-photo-480w.webp") {
		t.Fatal(got)
	}
}

func TestLocalSafePaths(t *testing.T) {
	l, err := NewLocal(t.TempDir(), "/uploads")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", "../escape", "a/../../escape", "/absolute", `a\..\escape`, "C:/escape", "a/./b", "a//b", "a\x00b"} {
		if _, err := l.Open(context.Background(), key); err == nil {
			t.Fatalf("accepted %q", key)
		}
	}
	key := VariantKey("原图.jpg", "hash", 1, 200)
	if _, err := l.Save(context.Background(), key, strings.NewReader("data"), 4, "image/webp"); err != nil {
		t.Fatal(err)
	}
	r, err := l.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, _ := io.ReadAll(r)
	if string(b) != "data" {
		t.Fatal(string(b))
	}
}

func TestStaticImageConfig(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 600, 300))); err != nil {
		t.Fatal(err)
	}
	cfg, err := StaticImageConfig(buf.Bytes())
	if err != nil || cfg.Width != 600 {
		t.Fatalf("%v %v", cfg, err)
	}
	animated := append([]byte{}, buf.Bytes()[:8]...)
	chunk := make([]byte, 20)
	binary.BigEndian.PutUint32(chunk, 8)
	copy(chunk[4:], "acTL")
	animated = append(animated, chunk...)
	animated = append(animated, buf.Bytes()[8:]...)
	if _, err := StaticImageConfig(animated); err == nil {
		t.Fatal("animated PNG accepted")
	}
	webp := []byte("RIFF0000WEBPVP8X")
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, 1)
	webp = append(webp, size...)
	webp = append(webp, 2, 0)
	if _, err := StaticImageConfig(webp); err == nil {
		t.Fatal("animated WebP accepted")
	}
	for _, data := range [][]byte{[]byte("GIF89a"), []byte("<svg></svg>")} {
		if _, err := StaticImageConfig(data); err == nil {
			t.Fatal("unsupported accepted")
		}
	}
}

func TestThumbnailEncoderFailure(t *testing.T) {
	th, err := NewThumbnailer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CWEBP_BIN", path.Join(t.TempDir(), "missing-cwebp"))
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	_, err = th.Get(context.Background(), "original", 4, func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(buf.Bytes())), nil })
	if err == nil {
		t.Fatal("encoder failure swallowed")
	}
	if th.Cached("original", 4) {
		t.Fatal("failed output cached")
	}
}

func TestPurgeSource(t *testing.T) {
	th, err := NewThumbnailer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []int{1, 200, 480, 740, 1080, 2560} {
		if err := os.WriteFile(th.Path("parent", w), []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(th.Path("other", 480), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := th.Purge("parent"); err != nil {
		t.Fatal(err)
	}
	for _, w := range []int{1, 200, 480, 740, 1080, 2560} {
		if th.Cached("parent", w) {
			t.Fatalf("cache remains %d", w)
		}
	}
	if !th.Cached("other", 480) {
		t.Fatal("other cache removed")
	}
}
