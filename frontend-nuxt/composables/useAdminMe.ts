import type { AdminUser } from '~/types'

// 后台当前用户:布局与页面共享。并发请求只发一次,切换后台页面不重复拉 /auth/me。
let inflight: Promise<AdminUser | null> | null = null

export function useAdminMe() {
  const me = useState<AdminUser | null>('admin_me', () => null)

  async function load(force = false): Promise<AdminUser | null> {
    if (!force && me.value) return me.value
    if (inflight) return inflight
    const { api } = useAuth()
    inflight = api<AdminUser>('/auth/me')
      .then((user) => {
        me.value = user
        return user
      })
      .catch(() => null)
      .finally(() => {
        inflight = null
      })
    return inflight
  }

  return { me, load }
}
