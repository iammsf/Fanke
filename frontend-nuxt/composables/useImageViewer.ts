// 图片插入/展示组件(image-viewer 插件)前端集成。
// 插件标签统一为 <fanke-img>说明</fanke-img>,与原生 <img> 互不干扰。
// 插件页可开启"替换默认 img 标签":开启后普通 <img> 也按插件样式渲染。
// 文章页与编辑器预览共用同一转换;默认配置来自插件公开 store(key="config"),标签内属性优先。

export interface IvConfig {
  des_pos: string
  des_sty: string
  replace_img: boolean
}

export interface IvStyle {
  hidden: boolean
  cap: Record<string, string>
  fig: Record<string, string>
  img: Record<string, string>
}

const RE_NUM = /^\d+(\.\d+)?$/
const RE_COLOR = /^(#[0-9a-fA-F]{3,8}|rgba?\([\d\s,.%]+\)|[a-zA-Z]{1,20})$/
const RE_UNSAFE_PROTO = /^\s*(?:javascript|vbscript):/i

const IMG_ATTR_WHITELIST = [
  'src',
  'srcset',
  'sizes',
  'width',
  'height',
  'alt',
  'title',
  'loading',
  'decoding',
  'data-orig',
] as const

// 仅在括号外按逗号/空白/分号拆分 token,避免破坏 rgb(0, 0, 0) / rgba(0, 0, 0, 0.5)
function splitStyleTokens(sty: string): string[] {
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

function safeNum(v: string): string {
  const s = v.trim()
  if (!RE_NUM.test(s)) return ''
  const n = Number(s)
  return Number.isFinite(n) ? s : ''
}

function safeColor(v: string): string {
  const s = v.trim()
  return RE_COLOR.test(s) ? s : ''
}

export function parseDesSty(sty: string): IvStyle {
  const out: IvStyle = { hidden: false, cap: {}, fig: {}, img: {} }
  for (const tok of splitStyleTokens(sty)) {
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
      if (k === 'fs') {
        const n = safeNum(v)
        if (n) out.cap['font-size'] = `${n}px`
      } else if (k === 'c') {
        const c = safeColor(v)
        if (c) out.cap['color'] = c
      } else if (k === 'bg') {
        const c = safeColor(v)
        if (c) out.cap['background'] = c
      } else if (k === 'bd') {
        const c = safeColor(v)
        if (c) out.cap['border'] = `1px solid ${c}`
      } else if (k === 'w') {
        const n = safeNum(v)
        if (n) out.fig['width'] = `${n}%`
      } else if (k === 'r') {
        const n = safeNum(v)
        if (n) out.img['border-radius'] = `${n}px`
      }
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

/**
 * 基于 DOMParser + 白名单属性拷贝把 <fanke-img>（及可选的普通 <img>）安全转换为 <figure>。
 * 彻底杜绝正则拆装 HTML 属性导致的属性逃逸 XSS。
 */
export function transformImageViewerHtml(html: string, cfg: IvConfig): string {
  if (!html) return html
  if (typeof DOMParser === 'undefined' || typeof document === 'undefined') return html

  const doc = new DOMParser().parseFromString(`<div id="__fanke_iv_root__">${html}</div>`, 'text/html')
  const root = doc.getElementById('__fanke_iv_root__')
  if (!root) return html

  const selector = cfg.replace_img ? 'fanke-img, img' : 'fanke-img'
  const targets = Array.from(root.querySelectorAll(selector))

  for (const el of targets) {
    if (!el.isConnected) continue
    const isPlugin = el.tagName.toLowerCase() === 'fanke-img'
    const rawSrc = (el.getAttribute('src') || '').trim()
    const src = rawSrc && !RE_UNSAFE_PROTO.test(rawSrc) ? rawSrc : ''

    if (!isPlugin) {
      if (!cfg.replace_img) continue
      if (!src) continue
      if (el.closest('a') || el.closest('figure.iv-img')) continue
    }

    // 插件标签为非自闭合自定义元素:仅采集首个子元素前的纯文本作图注,
    // 若历史数据存在未闭合 <fanke-img>,把被吞入的后续节点原样放回 figure 之后。
    let directText = ''
    const trailingNodes: Node[] = []
    if (isPlugin) {
      let hitElement = false
      for (const child of Array.from(el.childNodes)) {
        if (!hitElement && child.nodeType === 3) {
          directText += child.textContent || ''
        } else {
          hitElement = true
          trailingNodes.push(child)
        }
      }
    }

    const caption = directText.trim() || (el.getAttribute('des') || '').trim() || (el.getAttribute('alt') || '').trim()
    const pos = (el.getAttribute('des_pos') || cfg.des_pos || 'middle bottom').toLowerCase()
    const sty = mergeDesSty(cfg.des_sty, el.getAttribute('des_sty') || '')
    const vert = pos.includes('top') ? 'top' : 'bottom'
    const horiz = pos.includes('left') ? 'left' : pos.includes('right') ? 'right' : 'mid'
    const cls = `iv-img iv-pos-${vert} iv-align-${horiz}`

    const fig = doc.createElement('figure')
    fig.className = cls
    const figStyle = cssText(sty.fig)
    if (figStyle) fig.setAttribute('style', figStyle)

    if (src) {
      const img = doc.createElement('img')
      for (const name of IMG_ATTR_WHITELIST) {
        const val = el.getAttribute(name)
        if (val === null) continue
        if ((name === 'src' || name === 'data-orig') && RE_UNSAFE_PROTO.test(val)) continue
        img.setAttribute(name, val)
      }
      const imgStyle = cssText(sty.img)
      if (imgStyle) img.setAttribute('style', imgStyle)
      if (!img.hasAttribute('loading')) img.setAttribute('loading', 'lazy')
      fig.appendChild(img)
    } else {
      const sk = doc.createElement('div')
      sk.className = 'iv-skeleton'
      sk.setAttribute('title', '点击设置图片')
      sk.textContent = '🖼 点击设置图片'
      fig.appendChild(sk)
    }

    if (!sty.hidden) {
      const cap = doc.createElement('figcaption')
      cap.className = 'iv-cap'
      const capStyle = cssText(sty.cap)
      if (capStyle) cap.setAttribute('style', capStyle)
      cap.textContent = caption
      fig.appendChild(cap)
    }

    el.replaceWith(fig, ...trailingNodes)
  }

  return root.innerHTML
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
    return transformImageViewerHtml(html, config.value)
  }

  return { config, load, transform }
}
