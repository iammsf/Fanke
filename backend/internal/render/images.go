package render

import (
	"bytes"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ImgRewrite 正文图片改写配置。
type ImgRewrite struct {
	// Thumb 生成缩图地址(width 为像素宽度);返回 "" 表示该图不支持缩图。
	Thumb func(src string, width int) string
	// Size 返回源图原始宽高;0 表示未知(拿不到就不注入 width/height,避免写错比例)。
	Size func(src string) (int, int)
	// Widths 供 srcset 使用的宽度档位(升序)。
	Widths []int
	// Sizes srcset 的 sizes 描述,空则用默认正文宽度。
	Sizes string
}

const defaultProseSizes = "(max-width: 720px) 92vw, 700px"

// RewriteImages 把正文 HTML 里的 <img> 改写为「缩图 + srcset + 显式宽高 + 懒加载」。
//
// 只改 img 标签本身,其余节点(含已有 class/style、插件包裹的 .iv-img 结构)原样保留:
//   - src 换成中等档缩图;srcset 列出各档缩图 + 原图(原图放最大档,供高分屏取用)
//   - 已知原始尺寸时写 width/height —— 配合全局 css 的 height:auto 即可消除 CLS
//   - loading=lazy / decoding=async;首屏外的图不必抢带宽
//
// 解析失败(极端脏 HTML)时原样返回,绝不因优化把正文吞掉。
func RewriteImages(src string, cfg ImgRewrite) string {
	if strings.TrimSpace(src) == "" || cfg.Thumb == nil {
		return src
	}
	ctx := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(bridgeImages(src)), ctx)
	if err != nil {
		return src
	}
	sizes := cfg.Sizes
	if sizes == "" {
		sizes = defaultProseSizes
	}
	widths := cfg.Widths
	if len(widths) == 0 {
		widths = []int{480, 740, 1080}
	}
	changed := false
	for _, n := range nodes {
		walkImages(n, func(img *html.Node) {
			if rewriteOne(img, cfg, widths, sizes) {
				changed = true
			}
		})
	}
	if !changed {
		return src
	}
	var buf bytes.Buffer
	for _, n := range nodes {
		if err := html.Render(&buf, n); err != nil {
			return src
		}
	}
	return restoreImages(buf.String())
}

// ImageSrcs 收集 HTML 中所有 <img src>(去重,保持出现顺序)。
// 用于批量查尺寸:一次查询覆盖整篇正文,避免逐图查库。
func ImageSrcs(src string) []string {
	if strings.TrimSpace(src) == "" {
		return nil
	}
	ctx := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(src), ctx)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	out := []string{}
	for _, n := range nodes {
		walkImages(n, func(img *html.Node) {
			s := strings.TrimSpace(attrOf(img, "src"))
			if s == "" || strings.HasPrefix(s, "data:") || seen[s] {
				return
			}
			seen[s] = true
			out = append(out, s)
		})
	}
	return out
}

func walkImages(n *html.Node, fn func(*html.Node)) {
	if n.Type == html.ElementNode && (n.DataAtom == atom.Img || strings.EqualFold(n.Data, fankeImgTag)) {
		fn(n)
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkImages(c, fn)
	}
}

func attrOf(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}

func setAttr(n *html.Node, name, val string) {
	for i := range n.Attr {
		if strings.EqualFold(n.Attr[i].Key, name) {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: name, Val: val})
}

func rewriteOne(img *html.Node, cfg ImgRewrite, widths []int, sizes string) bool {
	src := strings.TrimSpace(attrOf(img, "src"))
	if src == "" || strings.HasPrefix(src, "data:") {
		return false
	}
	// 已有 srcset 说明内容里自带响应式方案,保持原样不覆盖
	if attrOf(img, "srcset") != "" {
		return false
	}
	ow, oh := 0, 0
	if cfg.Size != nil {
		ow, oh = cfg.Size(src)
	}

	var srcset []string
	for _, w := range widths {
		if ow > 0 && w >= ow {
			continue // 不放大:原图比该档窄时省掉这档
		}
		if u := cfg.Thumb(src, w); u != "" {
			srcset = append(srcset, u+" "+strconv.Itoa(w)+"w")
		}
	}
	if len(srcset) == 0 && ow > 0 {
		// 源图比所有档位都窄:用源图宽度当唯一档位(服务端只缩不放)
		if u := cfg.Thumb(src, ow); u != "" {
			srcset = append(srcset, u+" "+strconv.Itoa(ow)+"w")
		}
	}
	if len(srcset) == 0 {
		return false // 不支持缩图(如 gif/svg):保持原样
	}
	fallback := 740
	if ow > 0 && ow < fallback {
		fallback = ow
	}
	if u := cfg.Thumb(src, fallback); u != "" {
		setAttr(img, "src", u)
	}
	// 原图地址留在 data-orig:图片查看器要用它打开全分辨率大图,
	// 否则点开只会看到 740w 的缩略图(等于把看图功能做瘸了)。
	setAttr(img, "data-orig", src)
	setAttr(img, "srcset", strings.Join(srcset, ", "))
	setAttr(img, "sizes", sizes)
	if ow > 0 && oh > 0 {
		setAttr(img, "width", strconv.Itoa(ow))
		setAttr(img, "height", strconv.Itoa(oh))
	}
	if attrOf(img, "loading") == "" {
		setAttr(img, "loading", "lazy")
	}
	if attrOf(img, "decoding") == "" {
		setAttr(img, "decoding", "async")
	}
	return true
}
