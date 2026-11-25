package app

import (
	"testing"
	"time"

	"fanke/backend/internal/repo"
)

func TestPostRefTime(t *testing.T) {
	created := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	published := time.Date(2026, 11, 20, 12, 30, 0, 0, time.UTC)

	pWithoutPub := &repo.Post{CreatedAt: created}
	if got := postRefTime(pWithoutPub); !got.Equal(created) {
		t.Fatalf("got %v, want %v", got, created)
	}

	pWithPub := &repo.Post{CreatedAt: created, PublishedAt: &published}
	if got := postRefTime(pWithPub); !got.Equal(published) {
		t.Fatalf("got %v, want %v", got, published)
	}
}
