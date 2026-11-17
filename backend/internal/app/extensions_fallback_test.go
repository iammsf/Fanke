package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fanke/backend/internal/config"
)

func TestSanitizeFallbackTemplate(t *testing.T) {
	clean, err := sanitizeFallbackTemplate(`<figure class="fb"><img src="{{src}}" alt="{{caption}}" onerror="alert(1)"><figcaption>{{caption}}</figcaption></figure><script>alert(1)</script>`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`src="{{src}}"`, `alt="{{caption}}"`, `>{{caption}}</figcaption>`} {
		if !strings.Contains(clean, want) {
			t.Fatalf("placeholder lost: %q not in %q", want, clean)
		}
	}
	for _, bad := range []string{"onerror", "<script", "alert(1)"} {
		if strings.Contains(clean, bad) {
			t.Fatalf("sanitizer kept %q: %q", bad, clean)
		}
	}
	if _, err := sanitizeFallbackTemplate(strings.Repeat("a", 2049)); err == nil {
		t.Fatal("oversize template must be rejected")
	}
	if _, err := sanitizeFallbackTemplate("   "); err == nil {
		t.Fatal("empty template must be rejected")
	}
}

func TestMergeTagFallbacks(t *testing.T) {
	base := map[string]tagFallback{"old-tag": {Slug: "other", Version: "1", HTML: "<img src=\"{{src}}\">"}}

	// 新插件登记自己的标签,别人的登记不动
	m, err := mergeTagFallbacks(base, "image-viewer", "1.7", map[string]string{"fanke-img": `<img src="{{src}}">`})
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 || m["old-tag"].Slug != "other" || m["fanke-img"].Slug != "image-viewer" || m["fanke-img"].Version != "1.7" {
		t.Fatalf("merge wrong: %+v", m)
	}

	// 所有权唯一:别的插件不能抢已登记的标签
	if _, err := mergeTagFallbacks(m, "thief", "1.0", map[string]string{"fanke-img": "<img>"}); err == nil {
		t.Fatal("tag ownership must be exclusive")
	}

	// 同插件新版本不再声明的标签要移除(声明即当前真相)
	m2, err := mergeTagFallbacks(m, "image-viewer", "1.8", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m2["fanke-img"]; ok {
		t.Fatal("dropped tag must be unregistered")
	}
	if _, ok := m2["old-tag"]; !ok {
		t.Fatal("other plugin's record must survive")
	}

	// 非法标签名
	if _, err := mergeTagFallbacks(base, "x", "1", map[string]string{"FankeImg": "<img>"}); err == nil {
		t.Fatal("illegal tag name must be rejected")
	}
}

func TestExtensionHealthy(t *testing.T) {
	root := t.TempDir()
	a := &App{Cfg: &config.Config{ExtensionsDir: root}}
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if a.extensionHealthy("demo", nil) {
		t.Fatal("missing index.html must be unhealthy")
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<p>x</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !a.extensionHealthy("demo", nil) {
		t.Fatal("index.html present must be healthy")
	}
	// 声明了编辑器能力但 editor.js/editor.ts 被删掉(文件损坏)→ 不在线
	if a.extensionHealthy("demo", []string{EditorRegister}) {
		t.Fatal("missing editor script must be unhealthy")
	}
	if err := os.WriteFile(filepath.Join(dir, "editor.ts"), []byte("// x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !a.extensionHealthy("demo", []string{EditorRegister}) {
		t.Fatal("editor.ts present must be healthy")
	}
	// 整个目录被手动删掉(卸载残留启用记录)→ 不在线
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if a.extensionHealthy("demo", nil) {
		t.Fatal("removed dir must be unhealthy")
	}
}
