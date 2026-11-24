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
      .replace(/'/g, '&#39;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')

  const RE_NUM = /^\d+(\.\d+)?$/
  const RE_COLOR = /^(#[0-9a-fA-F]{3,8}|rgba?\([\d\s,.%]+\)|[a-zA-Z]{1,20})$/
  const RE_UNSAFE_PROTO = /^\s*(?:javascript|vbscript):/i

  const safeNum = (v: unknown): string => {
    const s = String(v ?? '').trim()
    if (!s || !RE_NUM.test(s)) return ''
    const n = Number(s)
    return Number.isFinite(n) ? s : ''
  }

  const safeColor = (v: unknown): string => {
    const s = String(v ?? '').trim()
    return RE_COLOR.test(s) ? s : ''
  }

  const splitStyleTokens = (sty: string): string[] => {
    const out: string[] = []
    let cur = ''
    let depth = 0
    for (const ch of String(sty || '')) {
      if (ch === '(') {
        depth++
        cur += ch
      } else if (ch === ')') {
        if (depth > 0) depth--
        cur += ch
      } else if (depth === 0 && /[\s,;]/.test(ch)) {
        if (cur) {
          out.push(cur)
          cur = ''
        }
      } else {
        cur += ch
      }
    }
    if (cur) out.push(cur)
    return out
  }

  // des_sty 令牌解析（与前台 useImageViewer.parseDesSty 保持一致，强校验数值与颜色白名单）
  function parseSty(sty: string) {
    const o: any = {}
    for (const tok of splitStyleTokens(sty)) {
      if (!tok) continue
      if (tok === 'hidden') o.hidden = true
      else if (tok === 'b') o.bold = true
      else if (tok === 'i') o.italic = true
      else {
        const i = tok.indexOf('=')
        if (i < 1) continue
        const k = tok.slice(0, i)
        const v = tok.slice(i + 1)
        if (k === 'fs') {
          const n = safeNum(v)
          if (n) o.fs = n
        } else if (k === 'c') {
          const c = safeColor(v)
          if (c) o.c = c
        } else if (k === 'bg') {
          const c = safeColor(v)
          if (c) o.bg = c
        } else if (k === 'bd') {
          const c = safeColor(v)
          if (c) o.bd = c
        } else if (k === 'w') {
          const n = safeNum(v)
          if (n) o.w = n
        } else if (k === 'r') {
          const n = safeNum(v)
          if (n) o.r = n
        }
      }
    }
    return o
  }

  function buildSty(v: Record<string, any>) {
    const out: string[] = []
    if (v.hidden) out.push('hidden')
    if (v.bold) out.push('b')
    if (v.italic) out.push('i')
    const fs = safeNum(v.fs)
    const color = safeColor(v.color)
    const bg = safeColor(v.bg)
    const bd = safeColor(v.bd)
    const w = safeNum(v.w)
    const r = safeNum(v.r)
    if (fs) out.push(`fs=${fs}`)
    if (color) out.push(`c=${color}`)
    if (bg) out.push(`bg=${bg}`)
    if (bd) out.push(`bd=${bd}`)
    if (w) out.push(`w=${w}`)
    if (r) out.push(`r=${r}`)
    return out.join(' ')
  }

  function render(attrs: Record<string, string>, caption: string) {
    const rawSrc = attrOf(attrs, 'src').trim()
    const src = rawSrc && !RE_UNSAFE_PROTO.test(rawSrc) ? rawSrc : ''
    const desPos = (attrOf(attrs, 'des_pos') || 'middle bottom').toLowerCase()
    const sty = parseSty(attrOf(attrs, 'des_sty'))
    const vertTop = desPos.indexOf('top') >= 0
    const align = desPos.indexOf('left') >= 0 ? 'left' : desPos.indexOf('right') >= 0 ? 'right' : 'center'
    // 历史标签可能把图注写在 des 属性里，这里统一收敛成 figcaption 文本。
    const text = (caption || '').trim() || attrOf(attrs, 'des') || attrOf(attrs, 'alt')
    const fs = safeNum(sty.fs)
    const c = safeColor(sty.c) || '#909399'
    const bg = safeColor(sty.bg)
    const bd = safeColor(sty.bd)
    const w = safeNum(sty.w)
    const r = safeNum(sty.r) || '8'

    const capStyle = [
      `font-size:${fs ? fs + 'px' : '13.5px'}`,
      `color:${c}`,
      'padding:6px 2px',
      'line-height:1.5',
      `text-align:${align}`,
      sty.bold ? 'font-weight:700' : '',
      sty.italic ? 'font-style:italic' : '',
      bg ? 'background:' + bg : '',
      bd ? 'border:1px solid ' + bd : '',
      sty.hidden ? 'display:none' : '',
    ]
      .filter(Boolean)
      .join(';')
    const figStyle = `margin:18px 0;max-width:100%${w ? ';width:' + w + '%' : ''}`
    const imgStyle = `display:block;width:100%;border-radius:${r}px`
    const img = src
      ? `<img src="${esc(src)}" alt="${esc(text)}" style="${esc(imgStyle)}">`
      : '<span class="fanke-ph" style="display:block;padding:40px 0;border:1px dashed #c0c4cc;border-radius:8px;color:#909399;text-align:center">点击设置图片</span>'
    const cap = `<figcaption style="${esc(capStyle)}">${esc(text)}</figcaption>`
    const body = vertTop ? cap + img : img + cap
    const dp = attrOf(attrs, 'des_pos')
    const ds = attrOf(attrs, 'des_sty')
    return (
      `<figure data-fanke-node="fanke-img"` +
      (dp ? ` des_pos="${esc(dp)}"` : '') +
      (ds ? ` des_sty="${esc(ds)}"` : '') +
      ` style="${esc(figStyle)}">${body}</figure>`
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
