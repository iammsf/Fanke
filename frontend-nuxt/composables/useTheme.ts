// 明暗主题切换(localStorage 记忆,首屏脚本已防闪烁)
// data-theme: 站点自有令牌;html.dark: Element Plus 暗色变量 —— 两者同步切换
export function useTheme() {
  const toggle = () => {
    const root = document.documentElement
    const dark = root.dataset.theme === 'dark'
    if (dark) {
      delete root.dataset.theme
      root.classList.remove('dark')
    } else {
      root.dataset.theme = 'dark'
      root.classList.add('dark')
    }
    localStorage.setItem('fanke-theme', dark ? 'light' : 'dark')
  }
  return { toggle }
}
