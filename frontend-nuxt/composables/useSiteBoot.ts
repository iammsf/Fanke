// 布局启动逻辑:加载设置 → 注入站点主题(仅文章页)→ 可选注入用户插件 CSS/JS
// 插件 <style> 同名替换、离开布局卸载;插件 <script> 按内容去重,防止 SPA 往返布局重复执行。
const executedPluginScripts = new Set<string>()

function upsertPluginStyle(name: string, css: string) {
  const existing = Array.from(document.head.querySelectorAll('style[data-plugin]')).filter(
    (node) => (node as HTMLStyleElement).dataset.plugin === name,
  )
  const el = (existing[0] as HTMLStyleElement | undefined) ?? document.createElement('style')
  for (let i = 1; i < existing.length; i++) existing[i]?.remove()
  el.dataset.plugin = name
  el.textContent = css
  if (!el.isConnected) document.head.append(el)
}

function clearPluginStyles() {
  document.head.querySelectorAll('style[data-plugin]').forEach((el) => el.remove())
}

export function useSiteBoot(injectPlugins = false) {
  const { load } = useSettings()
  const api = usePublicApi()
  const { apply, clear } = useThemeCss()
  let disposed = false

  onMounted(async () => {
    clear('admin')
    const s = await load()
    if (disposed) return
    // 站点主题:只作用于文章页;后台主题在 admin 布局里单独注入
    await apply('site', s?.theme_site || s?.theme?.name || '')
    if (disposed) return
    if (injectPlugins) {
      const ps = (await api.plugins()) ?? []
      if (disposed) return
      const activeStyleNames = new Set<string>()
      for (const p of ps) {
        if (p.css) {
          activeStyleNames.add(p.name)
          upsertPluginStyle(p.name, p.css)
        }
        if (p.js) {
          const scriptKey = `${p.name}\0${p.js}`
          if (!executedPluginScripts.has(scriptKey)) {
            executedPluginScripts.add(scriptKey)
            const el = document.createElement('script')
            el.dataset.plugin = p.name
            el.textContent = p.js
            document.body.append(el)
          }
        }
      }
      // 清理已下线/不再提供 CSS 的旧插件样式节点
      document.head.querySelectorAll('style[data-plugin]').forEach((node) => {
        const name = (node as HTMLStyleElement).dataset.plugin || ''
        if (!activeStyleNames.has(name)) node.remove()
      })
    }
  })

  onBeforeUnmount(() => {
    disposed = true
    clear('site')
    if (injectPlugins) clearPluginStyles()
  })
}
