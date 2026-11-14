// 未安装时,所有页面重定向到安装向导(/setup 自身除外)
export default defineNuxtRouteMiddleware(async (to) => {
  if (to.path === '/setup') return
  const { check } = useSetup()
  if (await check()) return navigateTo('/setup')
})
