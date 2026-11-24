// 运行时主题注入:文章页与后台各自一个 style 节点,切布局时主动卸载避免样式串染。
// site / admin 由设置里的 theme_site / theme_admin 决定;内置主题不注入任何 CSS。
const BUILTIN = new Set(['', 'material', 'element-plus', 'builtin'])

const themeSeq: Record<'site' | 'admin', number> = {
  site: 0,
  admin: 0,
}

export function useThemeCss() {
  const api = usePublicApi()

  function clear(target: 'site' | 'admin') {
    themeSeq[target]++
    if (!import.meta.client) return
    document.getElementById(`runtime-theme-${target}`)?.remove()
  }

  async function apply(target: 'site' | 'admin', name?: string | null) {
    const my = ++themeSeq[target]
    const id = `runtime-theme-${target}`
    const slug = (name || '').trim()
    if (BUILTIN.has(slug)) {
      if (import.meta.client) document.getElementById(id)?.remove()
      return
    }
    let css = ''
    try {
      css = await api.themeFile(slug, target)
    } catch {
      css = ''
    }
    // 异步拉取期间若已切走布局或发起了更新的主题请求,放弃挂载
    if (my !== themeSeq[target] || !import.meta.client) return
    let el = document.getElementById(id) as HTMLStyleElement | null
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

  return { apply, clear }
}
