// Package migrate 把 WordPress 的 Gutenberg HTML 转为 Markdown 并搬运媒体。
package migrate

import (
	"strings"

	"golang.org/x/net/html"
)

// HTMLToMarkdown 尽力把文章 HTML 转成 Markdown(保留代码块语言标注)。
func HTMLToMarkdown(src string) string {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return src
	}
	var b strings.Builder
	ctx := &conv{out: &b}
	ctx.walkChildren(doc)
	return strings.TrimSpace(ctx.postProcess(b.String()))
}

type conv struct {
	out       *strings.Builder
	inPre     bool
	inLink    bool
	listStack []listState
}

type listState struct {
	ordered bool
	index   int
}

func (c *conv) postProcess(s string) string {
	s = strings.ReplaceAll(s, "\n\n\n\n", "\n\n")
	s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	return s
}

func (c *conv) walkChildren(n *html.Node) {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.walk(ch)
	}
}

func (c *conv) walk(n *html.Node) {
	switch n.Type {
	case html.CommentNode: // Gutenberg 区块注释直接丢弃
		return
	case html.TextNode:
		if c.inPre {
			c.out.WriteString(n.Data)
			return
		}
		t := normalizeSpace(n.Data)
		if t != "" {
			c.out.WriteString(t)
		}
		return
	case html.ElementNode:
		c.element(n)
		return
	case html.DocumentNode:
		c.walkChildren(n)
		return
	}
	c.walkChildren(n)
}

func (c *conv) element(n *html.Node) {
	tag := n.Data
	switch tag {
	case "script", "style", "noscript", "iframe", "form", "input", "button":
		return
	case "br":
		if c.inPre {
			c.out.WriteString("\n")
		} else {
			c.out.WriteString("  \n")
		}
		return
	case "hr":
		c.block(func() { c.out.WriteString("---") })
		return
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level := int(tag[1] - '0')
		c.block(func() {
			c.out.WriteString(strings.Repeat("#", level) + " ")
			c.walkChildren(n)
		})
	case "p":
		c.block(func() { c.walkChildren(n) })
	case "strong", "b":
		c.inlineWrap("**", n)
	case "em", "i":
		c.inlineWrap("*", n)
	case "del", "s", "strike":
		c.inlineWrap("~~", n)
	case "code":
		if c.inPre {
			c.walkChildren(n)
			return
		}
		inner := textOf(n)
		c.out.WriteString("`" + inner + "`")
	case "pre":
		c.preBlock(n)
	case "blockquote":
		c.block(func() {
			var sub strings.Builder
			save := c.out
			c.out = &sub
			c.walkChildren(n)
			c.out = save
			lines := strings.Split(strings.TrimSpace(sub.String()), "\n")
			for _, l := range lines {
				c.out.WriteString("> " + l + "\n")
			}
		})
	case "ul", "ol":
		state := listState{ordered: tag == "ol"}
		c.listStack = append(c.listStack, state)
		c.out.WriteString("\n")
		c.walkChildren(n)
		c.listStack = c.listStack[:len(c.listStack)-1]
		c.out.WriteString("\n")
	case "li":
		depth := len(c.listStack)
		indent := strings.Repeat("  ", max(0, depth-1))
		marker := "- "
		if depth > 0 && c.listStack[depth-1].ordered {
			c.listStack[depth-1].index++
			marker = itoa(c.listStack[depth-1].index) + ". "
		}
		c.out.WriteString(indent + marker)
		c.walkChildren(n)
		c.out.WriteString("\n")
	case "a":
		href := attr(n, "href")
		c.out.WriteString("[")
		saveLink := c.inLink
		c.inLink = true
		c.walkChildren(n)
		c.inLink = saveLink
		c.out.WriteString("](" + href + ")")
	case "img":
		src := attr(n, "src")
		alt := attr(n, "alt")
		c.out.WriteString("![" + alt + "](" + src + ")")
	case "table", "thead", "tbody", "tr", "figure", "figcaption", "span", "div":
		// div/table 等容器直接递归(wp-block-* 包裹层天然被拍平)
		if tag == "table" {
			c.table(n)
			return
		}
		if tag == "tr" {
			return
		}
		c.walkChildren(n)
	default:
		c.walkChildren(n)
	}
}

// table 转 GFM 管道表。
func (c *conv) table(n *html.Node) {
	var rows [][]string
	var walkRow func(*html.Node)
	walkRow = func(node *html.Node) {
		for ch := node.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type == html.ElementNode && ch.Data == "tr" {
				var cells []string
				for td := ch.FirstChild; td != nil; td = td.NextSibling {
					if td.Type == html.ElementNode && (td.Data == "td" || td.Data == "th") {
						cells = append(cells, strings.ReplaceAll(strings.TrimSpace(textOf(td)), "|", "\\|"))
					}
				}
				if len(cells) > 0 {
					rows = append(rows, cells)
				}
			} else if ch.Type == html.ElementNode {
				walkRow(ch)
			}
		}
	}
	walkRow(n)
	if len(rows) == 0 {
		return
	}
	c.out.WriteString("\n")
	for i, row := range rows {
		c.out.WriteString("| " + strings.Join(row, " | ") + " |\n")
		if i == 0 {
			c.out.WriteString("|" + strings.Repeat(" --- |", len(row)) + "\n")
		}
	}
	c.out.WriteString("\n")
}

func (c *conv) preBlock(n *html.Node) {
	lang := ""
	cls := attr(n, "class")
	if m := strings.Contains(cls, "language-"); m {
		lang = afterSub(cls, "language-")
		if i := strings.IndexAny(lang, " \t"); i >= 0 {
			lang = lang[:i]
		}
	}
	// Enlighter 插件:<pre class="enlighter-origin enlighter-xxx">
	if lang == "" && strings.Contains(cls, "enlighter") {
		for _, part := range strings.Fields(cls) {
			if strings.HasPrefix(part, "enlighter-") && part != "enlighter-origin" && part != "enlighter-default" {
				lang = strings.TrimPrefix(part, "enlighter-")
				break
			}
		}
	}
	code := textOf(n)
	code = strings.TrimRight(code, "\n")
	c.block(func() {
		c.out.WriteString("```" + lang + "\n" + code + "\n```")
	})
}

func (c *conv) block(fn func()) {
	trimTail(c.out)
	c.out.WriteString("\n\n")
	fn()
	c.out.WriteString("\n\n")
}

func (c *conv) inlineWrap(mark string, n *html.Node) {
	c.out.WriteString(mark)
	c.walkChildren(n)
	c.out.WriteString(mark)
}

func trimTail(b *strings.Builder) {
	s := b.String()
	t := strings.TrimRight(s, " \t\n")
	b.Reset()
	b.WriteString(t)
}

func normalizeSpace(s string) string {
	if strings.TrimSpace(s) == "" {
		if strings.Contains(s, "\n") {
			return " "
		}
		return ""
	}
	ws := make([]rune, 0, len(s))
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' {
			if !prevSpace {
				ws = append(ws, ' ')
			}
			prevSpace = true
			continue
		}
		prevSpace = false
		ws = append(ws, r)
	}
	return string(ws)
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
		if node.Type == html.ElementNode && node.Data == "br" {
			b.WriteString("\n")
		}
		for ch := node.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return b.String()
}

func afterSub(s, sub string) string {
	i := strings.Index(s, sub)
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(s[i+len(sub):])
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
