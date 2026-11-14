// 编辑器插件面板:后端只暴露"声明了 addEditorPanel 的已启用插件"清单,
// 面板 UI 与逻辑都在插件包内。
export interface EditorPanel {
  plugin_slug: string
  name: string
  description: string
}

let inFlight: Promise<EditorPanel[]> | null = null

export function useEditorPanels() {
  const panels = useState<EditorPanel[]>('fanke_editor_panels', () => [])
  const loaded = useState<boolean>('fanke_editor_panels_loaded', () => false)

  async function load(): Promise<EditorPanel[]> {
    if (inFlight) return inFlight
    const { api } = useAuth()
    inFlight = (async () => {
      try {
        const res = await api<{ panels?: EditorPanel[] }>('/admin/extensions/editor-panels')
        panels.value = Array.isArray(res?.panels) ? res.panels : []
      } catch {
        panels.value = []
      } finally {
        loaded.value = true
        inFlight = null
      }
      return panels.value
    })()
    return inFlight
  }

  return { panels, loaded, load }
}
