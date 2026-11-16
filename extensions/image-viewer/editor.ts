// 宿主注入的编辑器注册 API（仅类型声明，编译后不产生运行时代码）
interface FankeEditorApi {
  registerNode: (reg: any) => void
}
interface Window {
  FankeEditor?: FankeEditorApi
}

// image-viewer 插件的编辑器能力注册。
// 画布渲染规则、属性面板字段全部由本插件提供，核心编辑器不含任何图片插件知识。
;(function () {
  const host = window.FankeEditor
  if (!host) return

  const attrOf = (attrs: Record<string, string>, name: string) => attrs[name] || ''

  const esc = (s: string) =>
    String(s || '')
      .replace(/&/g, '&amp;')
      .replace(/"/g, '&quot;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')

  // des_sty 令牌解析（与前台 useImageViewer.parseDesSty 保持一致）
  function parseSty(sty: string) {
    const o: any = {}
    for (const tok of String(sty || '').split(/[,\s;]+/)) {
      if (!tok) continue
      if (tok === 'hidden') o.hidden = true
      else if (tok === 'b') o.bold = true
      else if (tok === 'i') o.italic = true
      else {
        const i = tok.indexOf('=')
        if (i < 1) continue
        const k = tok.slice(0, i)
        const v = tok.slice(i + 1)
        if (k === 'fs') o.fs = v
        else if (k === 'c') o.c = v
        else if (k === 'bg') o.bg = v
        else if (k === 'bd') o.bd = v
        else if (k === 'w') o.w = v
        else if (k === 'r') o.r = v
      }
    }
    return o
  }

  function buildSty(v: Record<string, any>) {
    const out: string[] = []
    if (v.hidden) out.push('hidden')
    if (v.bold) out.push('b')
    if (v.italic) out.push('i')
    if (v.fs) out.push(`fs=${v.fs}`)
    if (v.color) out.push(`c=${v.color}`)
    if (v.bg) out.push(`bg=${v.bg}`)
    if (v.bd) out.push(`bd=${v.bd}`)
    if (v.w) out.push(`w=${v.w}`)
    if (v.r) out.push(`r=${v.r}`)
    return out.join(' ')
  }

  function render(attrs: Record<string, string>, caption: string) {
    const src = attrOf(attrs, 'src')
    const desPos = (attrOf(attrs, 'des_pos') || 'middle bottom').toLowerCase()
    const sty = parseSty(attrOf(attrs, 'des_sty'))
    const vertTop = desPos.indexOf('top') >= 0
    const align = desPos.indexOf('left') >= 0 ? 'left' : desPos.indexOf('right') >= 0 ? 'right' : 'center'
    // 历史标签可能把图注写在 des 属性里，这里统一收敛成 figcaption 文本。
    const text = (caption || '').trim() || attrOf(attrs, 'des') || attrOf(attrs, 'alt')
    const capStyle = [
      `font-size:${sty.fs ? sty.fs + 'px' : '13.5px'}`,
      `color:${sty.c || '#909399'}`,
      'padding:6px 2px',
      'line-height:1.5',
      `text-align:${align}`,
      sty.bold ? 'font-weight:700' : '',
      sty.italic ? 'font-style:italic' : '',
      sty.bg ? 'background:' + sty.bg : '',
      sty.bd ? 'border:1px solid ' + sty.bd : '',
      sty.hidden ? 'display:none' : '',
    ]
      .filter(Boolean)
      .join(';')
    const figStyle = `margin:18px 0;max-width:100%${sty.w ? ';width:' + sty.w + '%' : ''}`
    const img = src
      ? `<img src="${esc(src)}" alt="${esc(text)}" style="display:block;width:100%;border-radius:${sty.r || 8}px">`
      : '<span class="fanke-ph" style="display:block;padding:40px 0;border:1px dashed #c0c4cc;border-radius:8px;color:#909399;text-align:center">点击设置图片</span>'
    const cap = `<figcaption style="${capStyle}">${esc(text)}</figcaption>`
    const body = vertTop ? cap + img : img + cap
    const dp = attrOf(attrs, 'des_pos')
    const ds = attrOf(attrs, 'des_sty')
    return (
      `<figure data-fanke-node="fanke-img"` +
      (dp ? ` des_pos="${esc(dp)}"` : '') +
      (ds ? ` des_sty="${esc(ds)}"` : '') +
      ` style="${figStyle}">${body}</figure>`
    )
  }

  // 画布上的 figcaption 文本是图注的唯一权威来源；des 只作为历史内容的兜底。
  function captionOf(el: HTMLElement): string {
    const cap = el.querySelector('figcaption')
    if (cap) return (cap.textContent || '').trim()
    return (el.getAttribute('des') || '').trim()
  }

  host.registerNode({
    tag: 'fanke-img',
    label: '图片',
    insertLabel: '插件图片（图注 / 位置 / 样式）',
    defaultAttrs: () => ({ des_pos: 'middle bottom' }),
    toEditor: (attrs, caption) => render(attrs, caption),
    fromEditor: (el) => {
      const img = el.querySelector('img')
      const attrs: Record<string, string> = { src: img ? img.getAttribute('src') || '' : '' }
      const dp = el.getAttribute('des_pos')
      const ds = el.getAttribute('des_sty')
      if (dp) attrs.des_pos = dp
      if (ds) attrs.des_sty = ds
      return { attrs, caption: captionOf(el) }
    },
    panel: {
      sections: [
        {
          title: '图片',
          fields: [
            { key: 'src', label: '图片地址', type: 'text', placeholder: 'https://… 或 /uploads/…', width: '100%' },
            { key: 'caption', label: '图注文字', type: 'text', placeholder: '显示在图片旁边的说明，可留空', width: '100%' },
          ],
        },
        {
          title: '位置',
          fields: [
            {
              key: 'vpos',
              label: '图注位置',
              type: 'select',
              options: [
                { label: '图片下方', value: 'bottom' },
                { label: '图片上方', value: 'top' },
              ],
            },
            {
              key: 'hpos',
              label: '图注对齐',
              type: 'select',
              options: [
                { label: '靠左', value: 'left' },
                { label: '居中', value: 'mid' },
                { label: '靠右', value: 'right' },
              ],
            },
            { key: 'hidden', label: '隐藏图注', type: 'checkbox' },
          ],
        },
        {
          title: '图注样式',
          fields: [
            { key: 'bold', label: '加粗', type: 'checkbox' },
            { key: 'italic', label: '斜体', type: 'checkbox' },
            { key: 'fs', label: '字号(px)', type: 'number', placeholder: '14' },
            { key: 'color', label: '文字颜色', type: 'color' },
            { key: 'bg', label: '背景色', type: 'color' },
            { key: 'bd', label: '边框色', type: 'color' },
          ],
        },
        {
          title: '图片尺寸',
          fields: [
            { key: 'w', label: '宽度(%)', type: 'number', placeholder: '100' },
            { key: 'r', label: '圆角(px)', type: 'number', placeholder: '8' },
          ],
        },
      ],
    },
    read: (el) => {
      const img = el.querySelector('img')
      const desPos = (el.getAttribute('des_pos') || 'middle bottom').toLowerCase()
      const sty = parseSty(el.getAttribute('des_sty') || '')
      return {
        src: img ? img.getAttribute('src') || '' : '',
        caption: captionOf(el),
        vpos: desPos.indexOf('top') >= 0 ? 'top' : 'bottom',
        hpos: desPos.indexOf('left') >= 0 ? 'left' : desPos.indexOf('right') >= 0 ? 'right' : 'mid',
        hidden: !!sty.hidden,
        bold: !!sty.bold,
        italic: !!sty.italic,
        fs: sty.fs || '',
        color: sty.c || '',
        bg: sty.bg || '',
        bd: sty.bd || '',
        w: sty.w || '',
        r: sty.r || '',
      }
    },
    apply: (el, v) => {
      const desPos = `${v.vpos === 'top' ? 'top' : 'bottom'} ${v.hpos || 'mid'}`
      const desSty = buildSty(v)
      const attrs: Record<string, string> = {
        src: v.src || '',
        des_pos: desPos,
        des_sty: desSty,
      }
      const caption = String(v.caption || '').trim()
      const tmp = document.createElement('template')
      tmp.innerHTML = render(attrs, caption)
      const next = tmp.content.firstElementChild as HTMLElement | null
      if (next && el.parentNode) el.parentNode.replaceChild(next, el)
    },
  })
})()
