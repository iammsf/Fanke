package app

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"fanke/backend/internal/config"
	"fanke/backend/internal/media"
	"fanke/backend/internal/repo"
)

func TestServeLocalMediaSandboxHeaders(t *testing.T) {
	dir := t.TempDir()
	local, err := media.NewLocal(dir, "/uploads")
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"2026/11/xss.svg": "<svg onload=alert(1)></svg>",
		"2026/11/doc.pdf": "%PDF-1.4",
		"2026/11/a.txt":   "<script>alert(1)</script>",
		"2026/11/pic.png": "png-bytes",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := &App{Local: local}
	r := chi.NewRouter()
	r.Get("/uploads/*", a.ServeLocalMedia)

	for _, p := range []string{"/uploads/2026/11/xss.svg", "/uploads/2026/11/doc.pdf", "/uploads/2026/11/a.txt"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s status = %d", p, w.Code)
		}
		if got := w.Header().Get("Content-Disposition"); got != "attachment" {
			t.Errorf("%s Content-Disposition = %q, want attachment", p, got)
		}
		if got := w.Header().Get("Content-Security-Policy"); got != "sandbox" {
			t.Errorf("%s CSP = %q, want sandbox", p, got)
		}
		if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s X-Content-Type-Options = %q, want nosniff", p, got)
		}
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/uploads/2026/11/pic.png", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("png status = %d", w.Code)
	}
	if got := w.Header().Get("Content-Disposition"); got != "" {
		t.Errorf("png Content-Disposition = %q, want empty", got)
	}
}

func TestUploadMediaRejectsFakeBitmap(t *testing.T) {
	for _, filename := range []string{"evil.png", "evil.jpg", "evil.gif", "evil.webp"} {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		part, err := mw.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte("<html><script>alert(1)</script></html>"))
		_ = mw.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/media", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		w := httptest.NewRecorder()
		(&App{}).UploadMedia(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s fake bitmap status = %d, want 400", filename, w.Code)
		}
	}
}

func TestCommentToHTMLEscapesHTMLAndStyles(t *testing.T) {
	raw := `<img src="https://evil.com/t.png" style="color:red" class="admin-badge">hello` + "\n" + `<b>line2</b>`
	got := toHTML(raw)
	if strings.Contains(got, "<img") || strings.Contains(got, "<b>") {
		t.Fatalf("raw HTML not escaped: %s", got)
	}
	if !strings.Contains(got, "&lt;img ") || !strings.Contains(got, "<br>") {
		t.Fatalf("unexpected escaped output: %s", got)
	}
}

func TestVisiblePostOwnershipIsolation(t *testing.T) {
	pub := &repo.Post{ID: 1, AuthorID: 10, IsPublic: true}
	priv := &repo.Post{ID: 2, AuthorID: 10, IsPublic: false}

	reqWith := func(u authedUser) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/posts/x", nil)
		if u.ID != 0 {
			r = r.WithContext(context.WithValue(r.Context(), userKey, u))
		}
		return r
	}

	if !visiblePost(reqWith(authedUser{}), pub) {
		t.Error("public post should be visible to anonymous")
	}
	if visiblePost(reqWith(authedUser{}), priv) {
		t.Error("private post must not be visible to anonymous")
	}
	if visiblePost(reqWith(authedUser{ID: 20, Role: "author"}), priv) {
		t.Error("private post must not be visible to another author (IDOR)")
	}
	if !visiblePost(reqWith(authedUser{ID: 10, Role: "author"}), priv) {
		t.Error("private post should be visible to its own author")
	}
	if !visiblePost(reqWith(authedUser{ID: 1, Role: "admin"}), priv) {
		t.Error("private post should be visible to admin")
	}
}

func TestExtensionStaticSandboxAndPageCSP(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html><head></head><body>hi</body></html>"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "favico.svg"), []byte("<svg onload=alert(1)></svg>"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "help.html"), []byte("<script>alert(1)</script>"), 0o644)

	a := &App{Cfg: &config.Config{ExtensionsDir: root, SiteURL: "https://blog.example.com"}}
	r := chi.NewRouter()
	r.Get("/ext/{slug}/page", a.ServeExtensionPage)
	r.Get("/ext/{slug}/assets/*", a.ServeExtensionAsset)

	for _, p := range []string{"/ext/demo/assets/favico.svg", "/ext/demo/assets/help.html"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s status = %d", p, w.Code)
		}
		if got := w.Header().Get("Content-Security-Policy"); got != "sandbox" {
			t.Errorf("%s CSP = %q, want sandbox", p, got)
		}
		if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s X-Content-Type-Options = %q, want nosniff", p, got)
		}
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ext/demo/page", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("page status = %d", w.Code)
	}
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self' https://blog.example.com 'unsafe-inline'") {
		t.Errorf("page CSP missing explicit origin: %s", csp)
	}
	body := w.Body.String()
	if !strings.Contains(body, "location.origin!=='null'") || !strings.Contains(body, "fanke:resize") || !strings.Contains(body, "ev.source!==parent") {
		t.Errorf("bridge script missing opaque-origin / resize compatibility: %s", body)
	}
}

func TestLogoutCSRFAndRefreshCookiePath(t *testing.T) {
	a := &App{Cfg: &config.Config{}}
	w1 := httptest.NewRecorder()
	a.Logout(w1, httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil))
	if w1.Code != http.StatusForbidden {
		t.Fatalf("Logout without X-Requested-With status = %d, want 403", w1.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("X-Requested-With", "fanke")
	w2 := httptest.NewRecorder()
	a.Logout(w2, req)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("Logout with header status = %d, want 204", w2.Code)
	}
	cookies := w2.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Path != "/api/v1/auth" {
		t.Fatalf("refresh cookie = %+v, want Path=/api/v1/auth", cookies)
	}
}

func TestValidUpdateURLAndCurlArgs(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_UPDATE_HTTP", "")
	ConfigureUpdateHTTP(false)
	if err := validUpdateURL("http://8.8.8.8/updates.json"); err == nil {
		t.Fatal("http:// update URL should be rejected by default")
	}
	if err := validUpdateURL("https://8.8.8.8/updates.json"); err != nil {
		t.Fatalf("https:// public update URL failed: %v", err)
	}
	t.Setenv("ALLOW_INSECURE_UPDATE_HTTP", "true")
	if err := validUpdateURL("http://8.8.8.8/updates.json"); err != nil {
		t.Fatalf("http:// update URL should pass when ALLOW_INSECURE_UPDATE_HTTP=true: %v", err)
	}

	args := buildCurlArgs("https://example.com/u.json", "/tmp/out", 1024, "example.com:443:93.184.216.34", false, "")
	joined := strings.Join(args, " ")
	for _, want := range []string{"-g", "--proto =https", "--resolve example.com:443:93.184.216.34", "-- https://example.com/u.json"} {
		if !strings.Contains(joined, want) {
			t.Errorf("buildCurlArgs missing %q in %q", want, joined)
		}
	}
}

func TestReplaceWebRootAtomicAndPreservesOnError(t *testing.T) {
	base := t.TempDir()
	dst := filepath.Join(base, "web")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "index.html"), []byte("old-version"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 源目录不存在时必须保留旧版 web 内容不被清空。
	if err := replaceWebRoot(filepath.Join(base, "missing-src"), dst); err == nil {
		t.Fatal("replaceWebRoot should fail for missing src")
	}
	if b, err := os.ReadFile(filepath.Join(dst, "index.html")); err != nil || string(b) != "old-version" {
		t.Fatalf("existing web root damaged on error: %q, %v", b, err)
	}

	src := filepath.Join(base, "new-web")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte("new-version"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := replaceWebRoot(src, dst); err != nil {
		t.Fatalf("replaceWebRoot failed: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "index.html")); err != nil || string(b) != "new-version" {
		t.Fatalf("replaced web root = %q, %v", b, err)
	}
}

func TestValidHostHeaderAndForwardedProto(t *testing.T) {
	if !validHostHeader("blog.example.com") || !validHostHeader("127.0.0.1:8081") || !validHostHeader("[::1]:8081") {
		t.Error("valid hosts rejected")
	}
	for _, bad := range []string{"", "evil.com; script-src *", "evil.com/path", "user@evil.com", "evil.com\r\nX: 1", "evil.com:99999"} {
		if validHostHeader(bad) {
			t.Errorf("invalid host accepted: %q", bad)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	r.Header.Set("X-Forwarded-Proto", "javascript")
	if got := safeForwardedProto(r, "https"); got != "https" {
		t.Errorf("safeForwardedProto(javascript) = %q, want https", got)
	}
	r.Header.Set("X-Forwarded-Proto", "http, https")
	if got := safeForwardedProto(r, "https"); got != "http" {
		t.Errorf("safeForwardedProto(http, https) = %q, want http", got)
	}
}
