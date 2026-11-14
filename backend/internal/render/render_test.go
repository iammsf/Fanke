package render

import (
	"strconv"
	"strings"
	"testing"
)

func TestSanitizePairedImagePlugin(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
		bad  []string
	}{
		{name: "paired", in: `<p><IMG src="/a.jpg" des_pos="bottom">说明 &amp; 文本</IMG></p>`, want: []string{`<img src="/a.jpg" des_pos="bottom">说明 &amp; 文本</img>`}},
		{name: "multiple and empty", in: `<img src="/a.jpg">一</img><img src="/b.jpg"></img>`, want: []string{`<img src="/a.jpg">一</img>`, `<img src="/b.jpg"></img>`}},
		{name: "consecutive images", in: `<img src="/a.jpg"><img src="/b.jpg">说明</img>`, want: []string{`<img src="/a.jpg">`, `<img src="/b.jpg">说明</img>`}},
		{name: "unsafe", in: `<img src="javascript:alert(1)" onerror="alert(1)">坏</img>`, bad: []string{"javascript:", "onerror", "坏</img>"}},
		{name: "self-closing image in wrapper", in: `<fanke-image><img src="/a.jpg" /></fanke-image>`, want: []string{`<img src="/a.jpg"/>`}},
		{name: "script text", in: `<script>var x = '<img src="/fake.jpg">假</img>'</script>`, bad: []string{`<img src="/fake.jpg">假</img>`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeHTML(tt.in)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Fatalf("SanitizeHTML() missing retained markup %q in %q", want, got)
				}
			}
			for _, bad := range tt.bad {
				if strings.Contains(strings.ToLower(got), strings.ToLower(bad)) {
					t.Fatalf("SanitizeHTML() retained unsafe content %q in %q", bad, got)
				}
			}
		})
	}
	if got, err := Markdown(`<img src="/md.jpg">Markdown说明</img>`); err != nil || !strings.Contains(got, `<img src="/md.jpg">Markdown说明</img>`) {
		t.Fatalf("Markdown() did not preserve paired image plugin: %q, %v", got, err)
	}
}

func TestSanitizeHTML(t *testing.T) {
	src := `<p class="lead" style="text-align:center">段落</p><table><tr><td>单元格</td></tr></table><img src="/uploads/a.jpg" alt="图片" data-orig="original.jpg" des="说明" onerror="alert(1)"><script>alert(1)</script><a href="javascript:alert(1)">危险链接</a>`
	got := SanitizeHTML(src)

	for _, want := range []string{
		`<p class="lead" style="text-align: center">段落</p>`,
		`<table><tr><td>单元格</td></tr></table>`,
		`<img src="/uploads/a.jpg" alt="图片" data-orig="original.jpg" des="说明">`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("SanitizeHTML() missing retained markup %q in %q", want, got)
		}
	}
	for _, unwanted := range []string{"<script", "onerror", "javascript:", `href="` + "javascript:"} {
		if strings.Contains(strings.ToLower(got), strings.ToLower(unwanted)) {
			t.Fatalf("SanitizeHTML() retained unsafe content %q in %q", unwanted, got)
		}
	}
}

func TestRewriteImagesPreservesPairedImages(t *testing.T) {
	cfg := ImgRewrite{
		Thumb: func(src string, width int) string {
			return src + "?w=" + strconv.Itoa(width)
		},
	}
	tests := []struct {
		name     string
		in       string
		want     []string
		closings int
	}{
		{
			name:     "paired with description attributes",
			in:       `<p><img src="/a.jpg" des_pos="bottom" des_sty="default">说明</img></p>`,
			want:     []string{`des_pos="bottom"`, `des_sty="default"`, `说明</img>`},
			closings: 1,
		},
		{
			name:     "paired without description attributes",
			in:       `<img src="/a.jpg">说明 &amp; 文本</img>`,
			want:     []string{`说明 &amp; 文本</img>`},
			closings: 1,
		},
		{
			name: "ordinary image",
			in:   `<img src="/a.jpg">普通图片`,
			want: []string{`普通图片`},
		},
		{
			name:     "ordinary and paired images",
			in:       `<img src="/ordinary.jpg"><img src="/a.jpg">说明</img>`,
			want:     []string{`src="/ordinary.jpg?w=740"`, `说明</img>`},
			closings: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RewriteImages(tt.in, cfg)
			for _, want := range append(tt.want, `src="/a.jpg?w=740"`, `srcset="/a.jpg?w=480 480w, /a.jpg?w=740 740w, /a.jpg?w=1080 1080w"`) {
				if !strings.Contains(got, want) {
					t.Fatalf("RewriteImages() missing markup %q in %q", want, got)
				}
			}
			if strings.Count(got, "</img>") != tt.closings || strings.Contains(got, imageBridgeTag) {
				t.Fatalf("RewriteImages() did not restore paired images correctly: %q", got)
			}
		})
	}
}

func TestRewriteImagesUnchangedReturnsSource(t *testing.T) {
	src := `<IMG src='/a.jpg'>说明</IMG>`
	for _, cfg := range []ImgRewrite{
		{},
		{Thumb: func(string, int) string { return "" }},
	} {
		if got := RewriteImages(src, cfg); got != src {
			t.Fatalf("RewriteImages() changed unsupported image: got %q, want %q", got, src)
		}
	}
}
