// 后台页面鉴权:无 token 且 refresh 失败 → 登录页
export default defineNuxtRouteMiddleware(async () => {
  const { ensureAuth } = useAuth()
  if (!(await ensureAuth())) return navigateTo('/admin/login')
})
