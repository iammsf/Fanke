package render

import (
	"strings"

	"golang.org/x/net/html"
)

// NormalizePluginImages 只为插件图片补闭合标签；位置与样式一律留给渲染时套用插件默认配置。
// 这样在插件里改了默认 des_pos/des_sty 后，已发布文章无需重新保存也会跟着更新。
// 普通图片及其他原始字节保持不变；返回值仍须经过 SanitizeHTML 或 Markdown 的安全清洗。
func NormalizePluginImages(src string) string {
	t := html.NewTokenizer(strings.NewReader(src))
	var out, text strings.Builder
	var image string
	var plugin bool
	var ignored []string
	flush := func(closing string) {
		if image == "" {
			return
		}
		if plugin || closing != "" {
			image, _ = normalizePluginImageTag(image, true)
			if closing == "" {
				closing = "</img>"
			}
		}
		out.WriteString(image)
		out.WriteString(text.String())
		out.WriteString(closing)
		image, plugin = "", false
		text.Reset()
	}
	for {
		typ := t.Next()
		raw := string(t.Raw())
		if typ == html.ErrorToken {
			flush("")
			out.WriteString(raw)
			return out.String()
		}
		var name string
		if typ == html.StartTagToken || typ == html.SelfClosingTagToken || typ == html.EndTagToken {
			n, _ := t.TagName()
			name = strings.ToLower(string(n))
		}
		if len(ignored) == 0 && image != "" {
			if typ == html.TextToken {
				text.WriteString(raw)
				continue
			}
			if typ == html.EndTagToken && name == "img" {
				flush(raw)
				continue
			}
			flush("")
		}
		if len(ignored) == 0 && name == "img" && (typ == html.StartTagToken || typ == html.SelfClosingTagToken) {
			image = raw
			_, plugin = normalizePluginImageTag(raw, false)
			continue
		}
		switch name {
		case "script", "style", "textarea", "pre", "code":
			if typ == html.StartTagToken {
				ignored = append(ignored, name)
			} else if typ == html.EndTagToken {
				for i := len(ignored) - 1; i >= 0; i-- {
					if ignored[i] == name {
						ignored = ignored[:i]
						break
					}
				}
			}
		}
		out.WriteString(raw)
	}
}

func imageSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

// normalizePluginImageTag 只判定是否为插件图片(带 des / des_sty / des_pos 属性),
// 不再往标签里写默认 des_pos:默认位置与样式改由前端渲染时套用插件配置。
func normalizePluginImageTag(raw string, force bool) (string, bool) {
	if force {
		return raw, true
	}
	return raw, hasPluginDesAttr(raw)
}

func hasPluginDesAttr(raw string) bool {
	i := 1
	for i < len(raw) && !imageSpace(raw[i]) && raw[i] != '/' && raw[i] != '>' {
		i++
	}
	for i < len(raw) {
		for i < len(raw) && (imageSpace(raw[i]) || raw[i] == '/') {
			i++
		}
		if i == len(raw) || raw[i] == '>' {
			return false
		}
		start := i
		for i < len(raw) && !imageSpace(raw[i]) && raw[i] != '=' && raw[i] != '>' && raw[i] != '/' {
			i++
		}
		name := strings.ToLower(raw[start:i])
		for i < len(raw) && imageSpace(raw[i]) {
			i++
		}
		if i < len(raw) && raw[i] == '=' {
			i++
			for i < len(raw) && imageSpace(raw[i]) {
				i++
			}
			if i < len(raw) && (raw[i] == '\'' || raw[i] == '"') {
				quote := raw[i]
				i++
				for i < len(raw) && raw[i] != quote {
					i++
				}
				if i < len(raw) {
					i++
				}
			} else {
				for i < len(raw) && !imageSpace(raw[i]) && raw[i] != '>' {
					i++
				}
			}
		}
		switch name {
		case "des", "des_sty", "des_pos":
			return true
		}
	}
	return false
}
