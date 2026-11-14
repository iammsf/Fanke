// 布局启动逻辑:加载设置 → 注入站点主题(仅文章页)→ 可选注入用户插件 CSS/JS
export function useSiteBoot(injectPlugins = false) {
  const { load } = useSettings()
  const api = usePublicApi()
  const { apply } = useThemeCss()

  onMounted(async () => {
    const s = await load()
    // 站点主题:只作用于文章页;后台主题在 admin 布局里单独注入
    await apply('site', s?.theme_site || s?.theme?.name || '')
    if (injectPlugins) {
      const ps = (await api.plugins()) ?? []
      for (const p of ps) {
        if (p.css) {
          const el = document.createElement('style')
          el.dataset.plugin = p.name
          el.textContent = p.css
          document.head.append(el)
        }
        if (p.js) {
          const el = document.createElement('script')
          el.dataset.plugin = p.name
          el.textContent = p.js
          document.body.append(el)
        }
      }
    }
  })
}
