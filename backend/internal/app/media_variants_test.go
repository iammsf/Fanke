package app

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"fanke/backend/internal/config"
	"fanke/backend/internal/media"
	"fanke/backend/internal/repo"
)

type deleteTestStore struct {
	deleted []string
	fail    string
	missing bool
}

func (s *deleteTestStore) Name() string { return "local" }
func (s *deleteTestStore) Save(context.Context, string, io.Reader, int64, string) (media.Object, error) {
	panic("not used")
}
func (s *deleteTestStore) Open(context.Context, string) (io.ReadCloser, error) { panic("not used") }
func (s *deleteTestStore) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	if key == s.fail {
		return errors.New("storage unavailable")
	}
	if s.missing {
		return os.ErrNotExist
	}
	return nil
}

func TestDeleteMediaFiles(t *testing.T) {
	variants := []repo.MediaVariant{{Key: "v480"}, {Key: "v740"}}
	s := &deleteTestStore{fail: "v740"}
	if err := deleteMediaFiles(context.Background(), s, "original", variants); err == nil {
		t.Fatal("failure swallowed")
	}
	if !reflect.DeepEqual(s.deleted, []string{"v480", "v740"}) {
		t.Fatalf("original removed on partial failure: %v", s.deleted)
	}
	s = &deleteTestStore{missing: true}
	if err := deleteMediaFiles(context.Background(), s, "original", variants); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.deleted, []string{"v480", "v740", "original"}) {
		t.Fatal(s.deleted)
	}
}

func TestSelectMediaVariant(t *testing.T) {
	variants := []repo.MediaVariant{{Width: 1080}, {Width: 480}, {Width: 740}}
	for _, tc := range []struct{ width, want int }{{32, 480}, {480, 480}, {600, 740}, {740, 740}, {1000, 1080}, {2560, 1080}} {
		v := selectMediaVariant(variants, tc.width)
		if v == nil || v.Width != tc.want {
			t.Fatalf("%d => %v", tc.width, v)
		}
	}
	if selectMediaVariant(nil, 480) != nil {
		t.Fatal("empty selection")
	}
	if v := selectMediaVariant([]repo.MediaVariant{{Width: 200}}, 1080); v.Width != 200 {
		t.Fatal(v)
	}
}

func TestBucketOwnership(t *testing.T) {
	for _, tc := range []struct {
		url, key, base string
		want           bool
	}{
		{"https://img.example/a.jpg", "a.jpg", "https://img.example", true},
		{"https://img.example/new/a.jpg", "a.jpg", "https://img.example/new/", true},
		{"https://img.example/old/a.jpg", "a.jpg", "https://img.example/new", false},
		{"https://img.example.evil/a.jpg", "a.jpg", "https://img.example", false},
		{"https://img.example/new/a.jpg", "new/a.jpg", "https://img.example/new", false},
	} {
		if got := belongsToBucket(tc.url, tc.key, tc.base); got != tc.want {
			t.Errorf("%+v => %v", tc, got)
		}
	}
}

func TestThumbWithoutWebPAccept(t *testing.T) {
	a := &App{Cfg: &config.Config{MediaBaseURL: "/uploads"}}
	r := httptest.NewRequest("GET", "/img?src=%2Fuploads%2Foriginal.png&w=480", nil)
	w := httptest.NewRecorder()
	a.ServeThumb(w, r)
	if w.Code != 302 || w.Header().Get("Location") != "/uploads/original.png" || w.Header().Get("Vary") != "Accept" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
}
