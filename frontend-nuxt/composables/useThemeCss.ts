// 运行时主题注入:文章页与后台各自一个 style 节点,互不覆盖。
// site / admin 由设置里的 theme_site / theme_admin 决定;内置主题不注入任何 CSS。
const BUILTIN = new Set(['', 'material', 'element-plus', 'builtin'])

export function useThemeCss() {
  const api = usePublicApi()

  async function apply(target: 'site' | 'admin', name?: string | null) {
    const id = `runtime-theme-${target}`
    let el = document.getElementById(id) as HTMLStyleElement | null
    const slug = (name || '').trim()
    if (BUILTIN.has(slug)) {
      el?.remove()
      return
    }
    let css = ''
    try {
      css = await api.themeFile(slug, target)
    } catch {
      css = ''
    }
    if (!css) {
      el?.remove()
      return
    }
    if (!el) {
      el = document.createElement('style')
      el.id = id
      document.head.append(el)
    }
    el.textContent = css
  }

  return { apply }
}
