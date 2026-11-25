package repo

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAdjacentPostSQL(t *testing.T) {
	prevSQL := adjacentPostSQL(true)
	for _, want := range []string{
		"status = 'published'",
		"is_public = TRUE",
		"(COALESCE(published_at, created_at), id) < ($1, $2)",
		"ORDER BY COALESCE(published_at, created_at) DESC, id DESC",
		"LIMIT 1",
	} {
		if !strings.Contains(prevSQL, want) {
			t.Fatalf("prev SQL missing %q:\n%s", want, prevSQL)
		}
	}

	nextSQL := adjacentPostSQL(false)
	for _, want := range []string{
		"status = 'published'",
		"is_public = TRUE",
		"(COALESCE(published_at, created_at), id) > ($1, $2)",
		"ORDER BY COALESCE(published_at, created_at) ASC, id ASC",
		"LIMIT 1",
	} {
		if !strings.Contains(nextSQL, want) {
			t.Fatalf("next SQL missing %q:\n%s", want, nextSQL)
		}
	}
}

func TestPostNavItemJSON(t *testing.T) {
	item := PostNavItem{Slug: "hello-world", Title: "你好世界"}
	b, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	want := `{"slug":"hello-world","title":"你好世界"}`
	if string(b) != want {
		t.Fatalf("got %s, want %s", string(b), want)
	}
}
