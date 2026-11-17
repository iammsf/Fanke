// 图片插入/展示组件(image-viewer 插件)前端集成。
// 插件标签统一为 <fanke-img>说明</fanke-img>,与原生 <img> 互不干扰。
// 插件页可开启"替换默认 img 标签":开启后普通 <img> 也按插件样式渲染。
// 文章页与编辑器预览共用同一转换;默认配置来自插件公开 store(key="config"),标签内属性优先。

export interface IvConfig {
  des_pos: string
  des_sty: string
  replace_img: boolean
}

// 一次扫描两种标签:fanke-img(插件标签)与原生 img(仅在开启替换时处理)。
// 说明文字 = 标签内的连续文本,不能用 "<" 包裹其他标签。
const RE_ALL = /<(fanke-img)\b([^>]*)>(?:([^<]*?)<\/fanke-img\s*>)?|<img\b([^>]*)>/gi

const attr = (s: string, name: string): string => {
  const m = s.match(new RegExp(`\\b${name}\\s*=\\s*(?:"([^"]*)"|'([^']*)'|([^\\s>]+))`))
  return m ? (m[1] ?? m[2] ?? m[3] ?? '') : ''
}

interface IvStyle {
  hidden: boolean
  cap: Record<string, string>
  fig: Record<string, string>
  img: Record<string, string>
}

export function parseDesSty(sty: string): IvStyle {
  const out: IvStyle = { hidden: false, cap: {}, fig: {}, img: {} }
  for (const tok of (sty || '').split(/[,\s;]+/)) {
    if (!tok) continue
    if (tok === 'hidden') out.hidden = true
    else if (tok === 'b') out.cap['font-weight'] = '700'
    else if (tok === 'i') out.cap['font-style'] = 'italic'
    else {
      const i = tok.indexOf('=')
      if (i < 1) continue
      const k = tok.slice(0, i)
      const v = tok.slice(i + 1)
      if (!v) continue
      if (k === 'fs') out.cap['font-size'] = `${v}px`
      else if (k === 'c') out.cap['color'] = v
      else if (k === 'bg') out.cap['background'] = v
      else if (k === 'bd') out.cap['border'] = `1px solid ${v}`
      else if (k === 'w') out.fig['width'] = `${v}%`
      else if (k === 'r') out.img['border-radius'] = `${v}px`
    }
  }
  return out
}

// 合并默认样式与标签样式:标签只覆盖它写了的 token,其余沿用插件默认配置。
export function mergeDesSty(base: string, over: string): IvStyle {
  const a = parseDesSty(base)
  const b = parseDesSty(over)
  return {
    hidden: a.hidden || b.hidden,
    cap: { ...a.cap, ...b.cap },
    fig: { ...a.fig, ...b.fig },
    img: { ...a.img, ...b.img },
  }
}

const cssText = (o: Record<string, string>) =>
  Object.entries(o)
    .map(([k, v]) => `${k}:${v}`)
    .join(';')

// 判断某个位置是否位于未闭合的 <a> 内:替换原生 img 时跳过外链图片,避免把 figure 塞进链接。
const insideLink = (html: string, pos: number) => {
  let depth = 0
  const re = /<a\b[^>]*>|<\/a\s*>/gi
  const head = html.slice(0, pos)
  let m: RegExpExecArray | null
  while ((m = re.exec(head))) depth += m[0].startsWith('</') ? -1 : 1
  return depth > 0
}

export function useImageViewer() {
  // 认领 fanke-img:插件在线时由本文件渲染,账本回落(useTagFallbacks)不抢先替换;
  // 插件离线时认领不生效,标签照常按账本模板兜底。
  useCoreTagRenderers().claim('fanke-img')
  const base = (useRuntimeConfig().public.apiBase || '/api/v1').replace(/\/+$/, '')
  const config = useState<IvConfig | null>('fanke_iv_config', () => null)
  const tried = useState<boolean>('fanke_iv_tried', () => false)

  // 拉取插件默认配置(公开通道;插件未启用/未安装时静默失败,transform 退回原始 HTML)
  async function load() {
    if (tried.value) return
    tried.value = true
    try {
      const res = await fetch(`${base}/ext/image-viewer/hook/store.get_batch`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ keys: ['config'] }),
      })
      if (!res.ok) return
      const j = await res.json()
      // hook 统一响应结构是 {ok,data};兼容旧实现直接平铺 items 的形态。
      const v = j?.data?.items?.config ?? j?.items?.config
      config.value = {
        des_pos: typeof v?.des_pos === 'string' && v.des_pos ? v.des_pos : 'middle bottom',
        des_sty: typeof v?.des_sty === 'string' ? v.des_sty : '',
        replace_img: !!v?.replace_img,
      }
    } catch {
      /* 保持 null */
    }
  }

  /**
   * 把 html 中的 fanke-img 标签转换为 figure;开启 replace_img 时普通 img 也转换。
   * 插件未启用或拉不到配置时原样返回,不干预文章页。
   */
  function transform(html: string): string {
    if (!html) return html
    if (!config.value) return html
    const cfg = config.value
    const re = new RegExp(RE_ALL.source, 'gi')
    return html.replace(re, (whole, tagName, fAttrs, capClosed, nAttrs, offset) => {
      const isPlugin = !!tagName
      if (!isPlugin) {
        if (!cfg.replace_img) return whole
        if (!attr(nAttrs || '', 'src')) return whole
        if (insideLink(html, offset)) return whole
      }
      const attrs = isPlugin ? (fAttrs || '') : (nAttrs || '')
      const src = attr(attrs, 'src')
      const alt = attr(attrs, 'alt')
      const caption = ((isPlugin ? capClosed : '') || '').trim() || attr(attrs, 'des') || alt
      const pos = (attr(attrs, 'des_pos') || cfg.des_pos || 'middle bottom').toLowerCase()
      const sty = mergeDesSty(cfg.des_sty, attr(attrs, 'des_sty'))
      const vert = pos.includes('top') ? 'top' : 'bottom'
      const horiz = pos.includes('left') ? 'left' : pos.includes('right') ? 'right' : 'mid'
      const cls = `iv-img iv-pos-${vert} iv-align-${horiz}`
      const figStyle = cssText(sty.fig)
      const capStyle = cssText(sty.cap)
      const imgStyle = cssText(sty.img)
      const cap = sty.hidden ? '' : `<figcaption class="iv-cap"${capStyle ? ` style="${capStyle}"` : ''}>${caption}</figcaption>`
      // 原样透传其余属性(含服务端注入的 srcset/sizes/width/height 与 decoding),
      // 只剔除插件自身的 des* 标记与 style(style 由 des_sty 的 r= 重新生成)。
      const kept = attrs
        .replace(/\s+des(?:_pos|_sty)?\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+)/g, '')
        .replace(/\s+style\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+)/g, '')
        .trim()
      const hasLoading = /\bloading\s*=/.test(kept)
      const visual = src
        ? `<img${kept ? ` ${kept}` : ''}${imgStyle ? ` style="${imgStyle}"` : ''}${hasLoading ? '' : ' loading="lazy"'} />`
        : `<div class="iv-skeleton" title="点击设置图片">🖼 点击设置图片</div>`
      return `<figure class="${cls}"${figStyle ? ` style="${figStyle}"` : ''}>${visual}${cap}</figure>`
    })
  }

  return { config, load, transform }
}
