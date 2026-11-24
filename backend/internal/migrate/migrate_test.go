package migrate

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeLocalFilePathBlocksTraversal(t *testing.T) {
	root := t.TempDir()
	got, err := safeLocalFilePath(root, "/wp-content/uploads/2025/a.jpg")
	if err != nil {
		t.Fatalf("valid path rejected: %v", err)
	}
	if !strings.HasPrefix(got, filepath.Clean(root)+string(filepath.Separator)) {
		t.Fatalf("unexpected resolved path: %s", got)
	}

	for _, bad := range []string{
		"/../../etc/passwd",
		"/%2e%2e/%2e%2e/etc/passwd",
		"//etc/passwd",
		"/wp-content/../../../etc/passwd",
		"/wp-content/uploads/a\x00.jpg",
		"/..\\..\\etc\\passwd",
		"/",
	} {
		if p, err := safeLocalFilePath(root, bad); err == nil {
			t.Errorf("traversal path %q should be rejected, got %q", bad, p)
		}
	}
}
