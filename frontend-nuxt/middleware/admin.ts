// 管理员专属路由守卫:配合 auth 中间件使用,校验当前账号 role === 'admin',
// 否则提示无权访问并跳转回 /admin 仪表盘。
export default defineNuxtRouteMiddleware(async () => {
  const { load } = useAdminMe()
  const me = await load()
  if (!me) return navigateTo('/admin/login')
  if (me.role !== 'admin') {
    const { push } = useToast()
    push('无权访问：仅管理员可进入该页面', false)
    return navigateTo('/admin')
  }
})
