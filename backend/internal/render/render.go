// Package render 将 Markdown 渲染为经消毒的 HTML(代码块保留 language-* class 供前端 Shiki 高亮)。
package render

import (
	"bytes"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
	html "golang.org/x/net/html"
)

var policy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowElements("pre", "code", "table", "thead", "tbody", "tr", "th", "td", "figure", "figcaption", "del", "s", "sup", "sub", "hr", imageBridgeTag, fankeImgTag)
	p.AllowAttrs("class").OnElements("code", "pre", "span", "div", "table", "th", "td", "figure", "img", fankeImgTag, "a", "p", "h1", "h2", "h3", "h4", "h5", "h6")
	p.AllowAttrs("id").OnElements("h1", "h2", "h3", "h4", "h5", "h6")
	p.AllowAttrs("src", "alt", "title", "loading", "decoding", "width", "height", "srcset", "sizes", "data-orig").OnElements("img", fankeImgTag)
	p.AllowAttrs("style").OnElements("p", "div", "span", "h1", "h2", "h3", "h4", "h5", "h6", "img", "table", "th", "td", "figure", "figcaption")
	p.AllowStyles("text-align").Globally()
	p.AllowStyles("color", "background-color", "font-weight", "font-style", "text-decoration", "width", "height").OnElements("p", "div", "span", "img", "table", "th", "td", "figure", "figcaption")
	// 图片插入/展示组件(image-viewer 插件):插件标签 fanke-img 与原生 img 分开;
	// des* 说明属性在两种标签上都保留,便于历史内容兼容。
	p.AllowAttrs("des_pos", "des_sty", "des").OnElements("img", fankeImgTag)
	p.AllowAttrs("href", "title", "target", "rel").OnElements("a")
	p.AllowAttrs("start", "reversed").OnElements("ol")
	p.AddTargetBlankToFullyQualifiedLinks(true)
	return p
}()

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM, extension.Table, extension.Typographer, extension.Footnote),
	goldmark.WithRendererOptions(goldmarkhtml.WithUnsafe()),
)

// Markdown 渲染并消毒。
func Markdown(src string) (string, error) {
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	return sanitize(buf.String()), nil
}

// HTML 仅消毒(用于导入的 Gutenberg 残留 HTML)。
func SanitizeHTML(src string) string {
	return sanitize(src)
}

const imageBridgeTag = "fanke-image"

// fankeImgTag 是 image-viewer 插件的正文标签,与原生 img 区分,避免样式/语义互相干扰。
const fankeImgTag = "fanke-img"

func sanitize(src string) string {
	bridged := bridgeImages(src)
	return restoreImages(policy.Sanitize(bridged))
}

// bridgeImages 使用分词器，避免将注释和 script/style 文本误判为标记。
// 仅转换精确的 img-开始、文本、img-结束序列。
func bridgeImages(src string) string {
	t := html.NewTokenizer(strings.NewReader(src))
	var out strings.Builder
	var img, des string
	for {
		typ := t.Next()
		if typ == html.ErrorToken {
			if img != "" {
				out.WriteString(img)
				out.WriteString(des)
			}
			out.WriteString(string(t.Raw()))
			break
		}
		raw := string(t.Raw())
		if img == "" {
			if typ == html.StartTagToken {
				name, _ := t.TagName()
				if strings.EqualFold(string(name), "img") {
					img = raw
					continue
				}
			}
			out.WriteString(raw)
			continue
		}
		if typ == html.StartTagToken {
			name, _ := t.TagName()
			if strings.EqualFold(string(name), "img") {
				out.WriteString(img)
				out.WriteString(des)
				img, des = raw, ""
				continue
			}
		}
		if des == "" && typ == html.TextToken {
			des = raw
			continue
		}
		if typ == html.EndTagToken {
			name, _ := t.TagName()
			if strings.EqualFold(string(name), "img") {
				out.WriteString("<" + imageBridgeTag + ">" + img + des + raw + "</" + imageBridgeTag + ">")
				img, des = "", ""
				continue
			}
		}
		out.WriteString(img)
		out.WriteString(des)
		img, des = "", ""
		out.WriteString(raw)
	}
	return out.String()
}

func restoreImages(src string) string {
	t := html.NewTokenizer(strings.NewReader(src))
	var out strings.Builder
	var inner strings.Builder
	inWrapper := false
	hasImage := false
	for {
		typ := t.Next()
		if typ == html.ErrorToken {
			if inWrapper {
				if hasImage {
					out.WriteString(inner.String())
					out.WriteString("</img>")
				}
			} else {
				out.WriteString(string(t.Raw()))
			}
			break
		}
		raw := string(t.Raw())
		if !inWrapper && typ == html.StartTagToken {
			name, _ := t.TagName()
			if strings.EqualFold(string(name), imageBridgeTag) {
				inWrapper, hasImage = true, false
				inner.Reset()
				continue
			}
		}
		if inWrapper {
			if typ == html.StartTagToken || typ == html.SelfClosingTagToken {
				name, _ := t.TagName()
				if strings.EqualFold(string(name), "img") {
					hasImage = true
				}
			}
			if typ == html.EndTagToken {
				name, _ := t.TagName()
				if strings.EqualFold(string(name), imageBridgeTag) {
					if hasImage {
						out.WriteString(inner.String())
						out.WriteString("</img>")
					}
					inWrapper = false
					continue
				}
			}
			inner.WriteString(raw)
			continue
		}
		out.WriteString(raw)
	}
	return out.String()
}
