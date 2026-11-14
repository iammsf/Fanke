package render

import (
	"strings"
	"testing"
)

func TestNormalizePluginImages(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"ordinary bytes", `<P><IMG SRC='a>b' />text &amp; &#62;<img src=x>tail</P>`, `<P><IMG SRC='a>b' />text &amp; &#62;<img src=x>tail</P>`},
		{"description", `<img src="a" des="说明">说明`, `<img src="a" des="说明">说明</img>`},
		{"style attribute", `<img des_sty=default><b>next</b>`, `<img des_sty=default></img><b>next</b>`},
		{"explicit pair", `<IMG SRC='a>b'>说明 &amp; &#62;</IMG>`, `<IMG SRC='a>b'>说明 &amp; &#62;</IMG>`},
		{"empty pair", `<img src=a></img>`, `<img src=a></img>`},
		{"explicit position", `<img DES_POS = 'top left' des='a>b'>caption</IMG>`, `<img DES_POS = 'top left' des='a>b'>caption</IMG>`},
		{"empty position", `<img des_pos="">tail`, `<img des_pos="">tail</img>`},
		{"blank position", "<img DES_POS = '\t &#32;'>", "<img DES_POS = '\t &#32;'></img>"},
		{"boolean position", `<img des_pos alt='x'>`, `<img des_pos alt='x'></img>`},
		{"unquoted empty position", `<img des_pos=>`, `<img des_pos=></img>`},
		{"unquoted explicit position", `<img des_pos=bottom>`, `<img des_pos=bottom></img>`},
		{"self closing plugin", `<IMG des='a > b' />caption<p>next</p>`, `<IMG des='a > b' />caption</img><p>next</p>`},
		{"self closing explicit pair", `<img src='a'/>text</img>`, `<img src='a'/>text</img>`},
		{"consecutive", `<img des=a>one<img src=b><img des_sty=x/>two<img src=c>three</img>`, `<img des=a>one</img><img src=b><img des_sty=x/>two</img><img src=c>three</img>`},
		{"comment boundary", `<img des=a>text<!-- next -->more`, `<img des=a>text</img><!-- next -->more`},
		{"nested markup is not a pair", `<img src=a>text<b>bold</b></img>`, `<img src=a>text<b>bold</b></img>`},
		{"attribute lookalikes", `<img data-des="x" title="des_pos=''"><image des=x>`, `<img data-des="x" title="des_pos=''"><image des=x>`},
		{"raw entity bytes", `<img des="a&amp;b" des_pos='top&#32;left'>a&lt;b &copy; &#x3e;`, `<img des="a&amp;b" des_pos='top&#32;left'>a&lt;b &copy; &#x3e;</img>`},
		{"duplicate positions", `<img des_pos='' DES_POS='top'>`, `<img des_pos='' DES_POS='top'></img>`},
		{"incomplete trailing tag", `<img des=x>caption<div`, `<img des=x>caption</img><div`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizePluginImages(tt.in)
			if got != tt.want {
				t.Fatalf("NormalizePluginImages() = %q, want %q", got, tt.want)
			}
			if again := NormalizePluginImages(got); again != got {
				t.Fatalf("not idempotent: first %q, second %q", got, again)
			}
		})
	}
}

func TestNormalizePluginImagesIgnoredContexts(t *testing.T) {
	for _, tag := range []string{"script", "style", "textarea", "pre", "code", "PRE", "CoDe"} {
		t.Run(tag, func(t *testing.T) {
			prefix := "<" + tag + ` class='a>b'><img des="fake">caption</img><code><img des_pos=''></code></` + tag + ">"
			in := prefix + `<img des=real>real`
			want := prefix + `<img des=real>real</img>`
			if got := NormalizePluginImages(in); got != want {
				t.Fatalf("NormalizePluginImages() = %q, want %q", got, want)
			}
		})
	}
	in := `<!-- <img des=x>fake</img> --><script>const x = '<img des=x>fake</img>';</script><textarea>&lt;img des=x&gt;</textarea>`
	if got := NormalizePluginImages(in); got != in {
		t.Fatalf("changed ignored content: %q", got)
	}
}

func TestNormalizePluginImagesBeforeSanitize(t *testing.T) {
	in := `<img src="/safe.jpg" des="caption" onerror="alert(1)">说明 &amp; 文本<script>alert(1)</script><img src="javascript:alert(1)" des=x>unsafe`
	normalized := NormalizePluginImages(in)
	if !strings.Contains(normalized, `onerror="alert(1)"`) {
		t.Fatal("normalization must not substitute for sanitization")
	}
	got := SanitizeHTML(normalized)
	if !strings.Contains(got, `<img src="/safe.jpg" des="caption">说明 &amp; 文本</img>`) {
		t.Fatalf("sanitizer bridge lost normalized plugin image: %q", got)
	}
	for _, bad := range []string{"onerror", "javascript:", "<script", imageBridgeTag} {
		if strings.Contains(got, bad) {
			t.Fatalf("sanitizer retained %q: %q", bad, got)
		}
	}
}
