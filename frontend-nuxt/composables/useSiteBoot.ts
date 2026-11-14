// 布局启动逻辑:加载设置 → 注入运行时主题(上传主题)→ 可选注入用户插件 CSS/JS
export function useSiteBoot(injectPlugins = false) {
  const { load } = useSettings()
  const api = usePublicApi()

  onMounted(async () => {
    const s = await load()
    // 上传主题:拉取 tokens.css 注入 <head> 尾部,同优先级下覆盖内置 element-plus
    const name = s?.theme?.name
    if (name && name !== 'element-plus') {
      const css = await api.themeTokens(name)
      if (css) {
        let el = document.getElementById('runtime-theme') as HTMLStyleElement | null
        if (!el) {
          el = document.createElement('style')
          el.id = 'runtime-theme'
          document.head.append(el)
        }
        el.textContent = css
      }
    }
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
