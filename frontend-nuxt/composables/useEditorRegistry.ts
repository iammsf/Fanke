// 编辑器插件注册表。
//
// 核心编辑器（RichTextEditor）不认识任何具体插件：插件包可选提供 editor.ts，
// 安装后编译为 editor.js，由这里按启用列表注入并执行，插件调用 registerNode()
// 把自己的标签、画布渲染、属性面板 schema 注册进来。
//
// 没装插件 → 注册表为空 → 编辑器只有「普通图片」。装了多个 → 各注册各的 tag。

export type FieldType = 'text' | 'number' | 'color' | 'select' | 'checkbox'

export interface PanelField {
  key: string
  label: string
  type: FieldType
  placeholder?: string
  width?: string
  options?: { label: string; value: string }[]
}

export interface PanelSection {
  title?: string
  fields: PanelField[]
}

export interface NodeRegistration {
  /** 落库的标签名，如 fanke-img */
  tag: string
  /** 面板/菜单显示名 */
  label: string
  /** 插入菜单文案，默认同 label */
  insertLabel?: string
  /** 插入时的默认属性 */
  defaultAttrs?: () => Record<string, string>
  /** 从媒体库选中后构造属性（默认 { src }） */
  attrsFromMedia?: (media: { url: string; filename: string; mime: string }) => Record<string, string>
  /** 画布 HTML：返回一个根节点，必须带 data-fanke-node="<tag>" */
  toEditor: (attrs: Record<string, string>, caption: string) => string
  /** 从画布节点读回属性与图注文本 */
  fromEditor: (el: HTMLElement) => { attrs: Record<string, string>; caption: string }
  /** 属性面板 schema */
  panel?: { sections: PanelSection[] }
  /** 从画布节点读取面板值 */
  read: (el: HTMLElement) => Record<string, any>
  /** 把面板值写回画布节点 */
  apply: (el: HTMLElement, values: Record<string, any>) => void
}

const registry = new Map<string, NodeRegistration>()

/** 供插件 editor.js 调用 */
export function registerNode(reg: NodeRegistration) {
  if (!reg?.tag || !reg.toEditor || !reg.fromEditor) return
  registry.set(reg.tag, reg)
}

/** 宿主 API，注入到 window 供插件脚本使用 */
export function installEditorHost() {
  if (!import.meta.client) return
  ;(window as any).FankeEditor = { registerNode }
}

export function useEditorRegistry() {
  const nodes = useState<Record<string, NodeRegistration>>('fanke_editor_nodes', () => ({}))
  const loading = useState<boolean>('fanke_editor_loading', () => false)
  const loadedOnce = useState<boolean>('fanke_editor_loaded', () => false)

  function sync() {
    nodes.value = Object.fromEntries(registry.entries())
  }

  // 按启用插件列表注入 editor.js；不存在的直接跳过（404 视为该插件未提供编辑器能力）。
  async function ensureLoaded() {
    installEditorHost()
    if (loading.value) return
    if (loadedOnce.value) {
      sync()
      return
    }
    loading.value = true
    try {
      const base = (useRuntimeConfig().public.apiBase || '/api/v1').replace(/\/+$/, '')
      const res = await fetch(`${base}/ext`)
      if (res.ok) {
        const j = await res.json()
        const slugs: string[] = Array.isArray(j?.editors) ? j.editors : []
        await Promise.all(
          slugs.map(
            (slug) =>
              new Promise<void>((resolve) => {
                const s = document.createElement('script')
                s.src = `${base}/ext/${encodeURIComponent(slug)}/page/editor.js`
                s.async = false
                s.onload = () => resolve()
                s.onerror = () => {
                  s.remove()
                  resolve()
                }
                document.head.appendChild(s)
              }),
          ),
        )
      }
    } catch {
      /* 插件加载失败不影响编辑器本体 */
    } finally {
      sync()
      loading.value = false
      loadedOnce.value = true
    }
  }

  return { nodes, ensureLoaded, sync }
}
