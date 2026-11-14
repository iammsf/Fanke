package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestUploadLimits(t *testing.T) {
	for _, tc := range []struct {
		ext   string
		limit int64
	}{
		{".jpg", 100 << 20}, {".PNG", 100 << 20}, {".gif", 100 << 20},
		{".svg", 100 << 20}, {".avif", 100 << 20}, {".mp4", 1 << 30},
		{".WEBM", 1 << 30}, {".mov", 1 << 30}, {".pdf", 100 << 20},
		{".zip", 100 << 20}, {".txt", 100 << 20}, {".csv", 100 << 20},
	} {
		if got := uploadLimit(tc.ext); got != tc.limit {
			t.Errorf("%s limit=%d want %d", tc.ext, got, tc.limit)
		}
	}
	if maxUploadRequest != (1025 << 20) {
		t.Fatal("request overhead mismatch")
	}
}

func TestHashUploadBoundary(t *testing.T) {
	for _, size := range []int{7, 8, 9} {
		data := bytes.Repeat([]byte("x"), size)
		f := bytes.NewReader(data)
		sha, n, err := hashUpload(f, 8)
		if size > 8 {
			if !errors.Is(err, errUploadTooLarge) || n != 9 {
				t.Fatalf("overflow: n=%d err=%v", n, err)
			}
			continue
		}
		want := sha256.Sum256(data)
		if err != nil || n != int64(size) || sha != hex.EncodeToString(want[:]) {
			t.Fatalf("hash: %s %d %v", sha, n, err)
		}
		remaining, _ := io.ReadAll(f)
		if !bytes.Equal(remaining, data) {
			t.Fatal("file not rewound")
		}
	}
}

func TestUploadRequestTooLarge(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	r.ContentLength = maxUploadRequest + 1
	w := httptest.NewRecorder()
	(&App{}).UploadMedia(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestUploadMultipartCleanup(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	t.Setenv("TMPDIR", tmp)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", "not-allowed.exe")
	_, _ = io.Copy(part, io.LimitReader(zeroReader{}, multipartMemory+1))
	_ = mw.Close()
	for _, malformed := range []bool{false, true} {
		data := body.Bytes()
		if malformed {
			data = data[:len(data)-20]
		}
		r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(data))
		r.Header.Set("Content-Type", mw.FormDataContentType())
		w := httptest.NewRecorder()
		(&App{}).UploadMedia(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d", w.Code)
		}
		files, err := os.ReadDir(tmp)
		if err != nil || len(files) != 0 {
			t.Fatalf("multipart temporary files leaked: %v %v", files, err)
		}
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }
