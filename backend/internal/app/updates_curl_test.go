package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func requireCurl(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl 不可用")
	}
}

// curl 是更新下载的首选通道(国内直连 GitHub 常超时),但它不走 probeHTTPClient,
// 重定向必须自己逐跳校验 —— 这条链一旦退回 -L,一个外部地址就能把请求引到内网。
func TestCurlOnceReportsRedirectWithoutFollowing(t *testing.T) {
	requireCurl(t)
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out")
	code, next, err := curlOnce(context.Background(), srv.URL+"/updates.json", dest, 1<<20)
	if err != nil {
		t.Fatalf("curlOnce: %v", err)
	}
	if code != http.StatusFound {
		t.Fatalf("状态码 = %d, 想 %d", code, http.StatusFound)
	}
	if want := "http://169.254.169.254/latest/meta-data/"; next != want {
		t.Fatalf("下一跳 = %q, 想 %q", next, want)
	}
	if hits != 1 {
		t.Fatalf("源站被访问 %d 次, 想 1 次(说明 curl 自己跟了重定向)", hits)
	}
}

func TestCurlToFileBlocksForbiddenTargets(t *testing.T) {
	requireCurl(t)
	for _, u := range []string{
		"http://127.0.0.1:8080/updates.json",
		"http://localhost/updates.json",
		"http://169.254.169.254/latest/meta-data/",
		"file:///etc/passwd",
	} {
		if err := curlToFile(context.Background(), u, filepath.Join(t.TempDir(), "out"), 1<<20); err == nil {
			t.Fatalf("%s 应该被拒绝", u)
		}
	}
}

func TestAbsoluteURL(t *testing.T) {
	got, err := absoluteURL("https://example.com/a/updates.json", "/b/pkg.zip")
	if err != nil {
		t.Fatalf("absoluteURL: %v", err)
	}
	if want := "https://example.com/b/pkg.zip"; got != want {
		t.Fatalf("绝对化相对 Location = %q, 想 %q", got, want)
	}
	if got, err := absoluteURL("https://example.com/a", "https://cdn.example.com/z.zip"); err != nil || got != "https://cdn.example.com/z.zip" {
		t.Fatalf("绝对 Location = %q, %v", got, err)
	}
	if _, err := absoluteURL("https://example.com/a", "://bad"); err == nil {
		t.Fatal("非法 Location 应该报错")
	}
}

func TestCurlOnceWritesBodyAndEnforcesSizeCap(t *testing.T) {
	requireCurl(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out")
	if _, _, err := curlOnce(context.Background(), srv.URL, dest, 3); err == nil {
		t.Fatal("超过 --max-filesize 应该失败")
	}
	if _, _, err := curlOnce(context.Background(), srv.URL, dest, 1<<20); err != nil {
		t.Fatalf("curlOnce: %v", err)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("读落盘文件: %v", err)
	}
	if string(b) != "payload" {
		t.Fatalf("落盘内容 = %q", b)
	}
}
