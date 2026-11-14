// 后台认证与带鉴权的 API 请求。
// access token 存 sessionStorage;refresh 依赖 httpOnly cookie(同域)。

const TOKEN_KEY = 'fanke_access'

// 并发共享同一个 refresh 请求,避免轮换互相作废
let refreshPromise: Promise<boolean> | null = null

export function useAuth() {
  const base = (useRuntimeConfig().public.apiBase || '/api/v1').replace(/\/+$/, '')
  const token = useState<string | null>('fanke_access', () => null)

  // SPA 下首次调用时从 sessionStorage 恢复
  if (import.meta.client && token.value === null) {
    token.value = sessionStorage.getItem(TOKEN_KEY)
  }

  const getToken = () => token.value
  const setToken = (t: string) => {
    token.value = t
    sessionStorage.setItem(TOKEN_KEY, t)
  }
  const clearToken = () => {
    token.value = null
    sessionStorage.removeItem(TOKEN_KEY)
  }

  const gotoLogin = () => {
    if (!location.pathname.startsWith('/admin/login')) location.replace('/admin/login')
  }

  async function doRefresh(): Promise<boolean> {
    try {
      const res = await fetch(`${base}/auth/refresh`, {
        method: 'POST',
        headers: { 'X-Requested-With': 'fanke' },
        credentials: 'same-origin',
      })
      if (!res.ok) return false
      const j = await res.json()
      if (!j?.access_token) return false
      setToken(j.access_token)
      return true
    } catch {
      return false
    }
  }

  function tryRefresh(): Promise<boolean> {
    if (!refreshPromise) {
      refreshPromise = doRefresh().finally(() => {
        refreshPromise = null
      })
    }
    return refreshPromise
  }

  // 核心请求:带 Bearer;401 时 refresh 后重试一次
  async function api<T = unknown>(path: string, init: RequestInit = {}, allowRetry = true): Promise<T> {
    const t = getToken()
    const headers = new Headers(init.headers)
    if (t) headers.set('Authorization', `Bearer ${t}`)
    if (init.body && !(init.body instanceof FormData) && !headers.has('Content-Type')) {
      headers.set('Content-Type', 'application/json')
    }
    let res: Response
    try {
      res = await fetch(base + path, { ...init, headers, credentials: 'same-origin' })
    } catch {
      throw Object.assign(new Error('网络错误,无法连接服务器'), { status: 0 })
    }
    if (res.status === 401 && allowRetry) {
      if (await tryRefresh()) return api<T>(path, init, false)
      clearToken()
      gotoLogin()
      throw Object.assign(new Error('未登录或登录已过期'), { status: 401 })
    }
    if (res.status === 204) return undefined as T
    const text = await res.text()
    let data: any = null
    try {
      data = text ? JSON.parse(text) : null
    } catch {
      /* 非 JSON 响应 */
    }
    if (!res.ok) {
      if (res.status === 401) {
        clearToken()
        gotoLogin()
      }
      // 后端处于引导模式(未配置/断库):送进安装/找回向导
      if (res.status === 503 && data?.error === 'not_configured') {
        clearToken()
        location.replace('/setup')
      }
      throw Object.assign(new Error(data?.error || `请求失败(${res.status})`), { status: res.status })
    }
    return data as T
  }

  async function login(identifier: string, password: string): Promise<void> {
    const res = await fetch(`${base}/auth/login`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      body: JSON.stringify({ identifier, password }),
    })
    const j = await res.json().catch(() => ({}))
    if (!res.ok) {
      const map: Record<number, string> = {
        400: '请输入用户名和密码',
        401: '用户名或密码错误',
        429: '尝试过于频繁,请稍后再试',
      }
      throw new Error(map[res.status] || j?.error || `登录失败(${res.status})`)
    }
    if (!j?.access_token) throw new Error('登录响应异常')
    setToken(j.access_token)
  }

  // 解码 JWT 的 exp,判断是否已过期(或即将过期)
  function isTokenExpired(token: string, slackSec = 30): boolean {
    try {
      const parts = token.split('.')
      if (parts.length !== 3) return false // 非 JWT 无法判断,交给后端
      const payload = JSON.parse(atob(parts[1]))
      if (!payload.exp) return false
      return payload.exp * 1000 < Date.now() + slackSec * 1000
    } catch {
      return false
    }
  }

  // 有有效 token 或能 refresh 即视为已登录;token 过期时主动 refresh,避免首个请求就 401
  async function ensureAuth(): Promise<boolean> {
    const t = getToken()
    if (t && !isTokenExpired(t)) return true
    if (await tryRefresh()) return true
    return false
  }

  async function logout(): Promise<void> {
    const t = getToken()
    try {
      await fetch(`${base}/auth/logout`, {
        method: 'POST',
        headers: t ? { Authorization: `Bearer ${t}` } : {},
        credentials: 'same-origin',
      })
    } catch {
      /* 忽略网络错误,本地照常退出 */
    }
    clearToken()
    gotoLogin()
  }

  return { token, getToken, setToken, clearToken, tryRefresh, api, login, ensureAuth, logout, gotoLogin, apiBase: base }
}
