package media

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestS3PathStyleAndRegion(t *testing.T) {
	if _, err := NewS3FromConfig(context.Background(), S3Config{Bucket: "photos"}); err != nil {
		t.Fatal(err)
	}
}

func TestS3StorageOperationsWithDisabledUploadConfig(t *testing.T) {
	requireS3PluginBinary(t)
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if !strings.Contains(r.Header.Get("Authorization"), "/test-region/s3/") {
			t.Error("configured region not signed")
		}
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, "original")
		}
	}))
	defer server.Close()
	ConfigurePluginRoot(filepath.Join("..", "..", "..", "extensions"))
	cfg := S3Config{Endpoint: server.URL, Region: "test-region", AccessKey: "key", SecretKey: "secret", Bucket: "photos", PublicBase: "https://cdn.example.test/", Enabled: false}
	store, err := NewS3FromConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if store.Name() != "s3" {
		t.Fatal("wrong storage name")
	}
	obj, err := store.Save(context.Background(), "image.txt", strings.NewReader("original"), 8, "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if obj.URL != "https://cdn.example.test/image.txt" {
		t.Fatal(obj.URL)
	}
	rc, err := store.Open(context.Background(), "image.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	if string(data) != "original" {
		t.Fatal("bad object")
	}
	if err := store.Delete(context.Background(), "image.txt"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "PUT /photos/image.txt,GET /photos/image.txt,DELETE /photos/image.txt" {
		t.Fatalf("operations: %v", calls)
	}
}

func testBool(v bool) *bool { return &v }

// requireS3PluginBinary 该用例会真起 s3-storage 子进程，而 extensions/*/server/bin 是 gitignore 的构建产物。
// 干净检出上直接 t.Fatal 会让 go test ./... 整体失败，所以这里跳过并给出构建命令。
func requireS3PluginBinary(t *testing.T) {
	t.Helper()
	root := os.Getenv("FAKE_S3_PLUGIN_DIR")
	if root == "" {
		root = filepath.Join("..", "..", "..", "extensions", "s3-storage")
	}
	entry := os.Getenv("FAKE_S3_PLUGIN_ENTRYPOINT")
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	if entry == "" {
		entry = filepath.Join("server", "bin", runtime.GOOS+"-"+runtime.GOARCH, "s3-storage-server"+exe)
	}
	if _, err := os.Stat(filepath.Join(root, entry)); err != nil {
		t.Skipf("缺少 s3-storage 插件二进制(%s)；先构建: cd extensions/s3-storage/server && go build -o bin/%s-%s/s3-storage-server%s .",
			entry, runtime.GOOS, runtime.GOARCH, exe)
	}
}
