// 插件标签回落账本的渲染端(纯函数,不依赖 Nuxt)。
//
// 账本由后端在安装/更新插件时按 info.json 的 fallback 字段登记(见 doc/plugins.md):
// 插件禁用、卸载或文件损坏时,/ext 的 enabled 里就没有它,渲染端按账本模板自动回落,
// 正文里已写进去的标签永远能渲染,不需要核心为每个插件写死回落分支。

export interface TagFallbackRec {
  tag: string
  slug: string
  html: string
}

export const escAttr = (s: string) =>
  String(s || '')
    .replace(/&/g, '&amp;')
    .replace(/"/g, '&quot;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')

// 落库形态标签:闭合优先,兼容未闭合(图注视为空)。与文章页 useImageViewer 的同款形状。
export const tagRe = (tag: string): RegExp =>
  new RegExp(`<${tag}\\b([^>]*)>(?:([^<]*?)<\\/${tag}\\s*>)?`, 'gi')

export const parseAttrs = (s: string): Record<string, string> => {
  const out: Record<string, string> = {}
  const re = /([a-zA-Z_:][a-zA-Z0-9_:.-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))/g
  let m: RegExpExecArray | null
  while ((m = re.exec(s || ''))) out[m[1].toLowerCase()] = m[2] ?? m[3] ?? m[4] ?? ''
  return out
}

// 占位符代入:{{caption}} 取标签内文本,其余 {{x}} 取同名属性;值一律转义。
// 属性名统一按小写比对(HTML 属性名本身不区分大小写)。
export const substitute = (tpl: string, attrs: Record<string, string>, caption: string): string =>
  tpl.replace(/\{\{\s*([a-zA-Z][a-zA-Z0-9_-]*)\s*\}\}/g, (_m, raw: string) => {
    const name = raw.toLowerCase()
    return escAttr(name === 'caption' ? caption : attrs[name] || '')
  })

const captionOf = (attrs: Record<string, string>, closed: string | undefined): string =>
  (closed || '').trim() || attrs.des || attrs.alt || ''

/**
 * 文章页回落:对每个「不在线且没有核心专用渲染器」的标签,把落库标签替换成账本模板。
 * skip 返回 true 的标签交给别的渲染器(插件在线时的核心集成代码),这里不碰。
 */
export function applyFallbacks(html: string, recs: TagFallbackRec[], skip: (rec: TagFallbackRec) => boolean): string {
  if (!html) return html
  for (const rec of recs) {
    if (skip(rec)) continue
    const re = tagRe(rec.tag)
    html = html.replace(re, (whole, attrs1, closed, attrs2) => {
      const attrs = parseAttrs(attrs1 ?? attrs2 ?? '')
      return substitute(rec.html, attrs, captionOf(attrs, closed))
    })
  }
  return html
}

/**
 * 编辑器画布回落:把不在线插件的落库标签包成信封节点——里面是账本模板的视觉,
 * data-fanke-orig 原样背着落库标签,保存时拆信还原,插件停用期间编辑正文不会丢标签。
 * 信封整体 contenteditable=false:视觉只是替身,改它写不回落库标签,与其悄悄丢掉用户的
 * 编辑,不如直接锁住,等插件恢复后再编辑。
 */
export function wrapFallbacks(html: string, recs: TagFallbackRec[], isDead: (rec: TagFallbackRec) => boolean): string {
  if (!html) return html
  for (const rec of recs) {
    if (!isDead(rec)) continue
    const re = tagRe(rec.tag)
    html = html.replace(re, (whole, attrs1, closed, attrs2) => {
      const attrs = parseAttrs(attrs1 ?? attrs2 ?? '')
      const visual = substitute(rec.html, attrs, captionOf(attrs, closed))
      const title = `插件 ${rec.slug} 未启用,此处按回落样式显示,不可编辑`
      return (
        `<div class="fanke-fallback-box" contenteditable="false"` +
        ` data-fanke-fallback="${escAttr(rec.tag)}" data-fanke-orig="${escAttr(whole)}" title="${escAttr(title)}">` +
        `${visual}</div>`
      )
    })
  }
  return html
}
