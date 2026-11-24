// 公开 API(浏览器同域 /api/v1,生产由反代直达后端)
import type {
  CommentItem,
  Page,
  Post,
  PostSummary,
  SetupStatus,
  SiteSettings,
  TermWithCount,
  ThemeInfo,
  UserPlugin,
} from '~/types'

export function usePublicApi() {
  const base = (useRuntimeConfig().public.apiBase || '/api/v1').replace(/\/+$/, '')
  const { ensurePublic, loaded, isEnabled } = useExtensions()
  const { prefetch } = useImageFocus()

  async function get<T>(path: string): Promise<T | null> {
    const r = await getWithStatus<T>(path)
    return r.ok ? r.data! : null
  }

  // 需要区分「不存在(404)」与「加载失败」时用这个。
  async function getWithStatus<T>(path: string): Promise<{ ok: boolean; status: number; data?: T }> {
    try {
      const headers: Record<string, string> = { Accept: 'application/json' }
      // 已登录后台时带上凭证,未公开文章(草稿)的预览靠它鉴权;匿名访客拿不到草稿。
      // 若本地 token 已过期则先主动续期,避免后端 optionalBearer 静默忽略导致草稿 404。
      if (import.meta.client) {
        const auth = useAuth()
        let token = auth.getToken() || sessionStorage.getItem('fanke_access')
        if (token && auth.isTokenExpired(token)) {
          const ok = await auth.ensureAuth()
          token = ok ? auth.getToken() : null
        }
        if (token) headers.Authorization = `Bearer ${token}`
      }
      const res = await fetch(base + path, {
        headers,
        signal: AbortSignal.timeout(8000),
        // 带鉴权头的预览请求禁缓存,避免私密草稿滞留浏览器缓存;匿名公开读沿用后端 Cache-Control
        cache: headers.Authorization ? 'no-store' : 'default',
      })
      if (!res.ok) return { ok: false, status: res.status }
      return { ok: true, status: res.status, data: (await res.json()) as T }
    } catch {
      return { ok: false, status: 0 }
    }
  }

  async function getText(path: string): Promise<string> {
    try {
      const res = await fetch(base + path, { signal: AbortSignal.timeout(8000), cache: 'no-store' })
      return res.ok ? await res.text() : ''
    } catch {
      return ''
    }
  }

  return {
    async posts(params: { page?: number; per?: number; tag?: string; category?: string; q?: string }) {
      const extensionsReady = ensurePublic()
      const u = new URLSearchParams()
      if (params.page) u.set('page', String(params.page))
      if (params.per) u.set('per', String(params.per))
      if (params.tag) u.set('tag', params.tag)
      if (params.category) u.set('category', params.category)
      if (params.q) u.set('q', params.q)
      const result = await get<Page<PostSummary>>(`/posts?${u}`)
      if (result) {
        void extensionsReady.then(() => {
          if (loaded.value && isEnabled('image-focus')) {
            prefetch(result.items.map((item) => item.cover_url).filter((url): url is string => !!url))
          }
        })
      }
      return result
    },
    post: (slug: string) =>
      getWithStatus<{ post: Post; comments: number }>(`/posts/${encodeURIComponent(slug)}`),
    terms: (type: 'category' | 'tag') => get<TermWithCount[]>(`/terms?type=${type}`),
    settings: () => get<SiteSettings>('/settings'),
    comments: (slug: string) =>
      get<{ items: CommentItem[]; allow_comments: boolean }>(`/posts/${encodeURIComponent(slug)}/comments`),
    themes: () => get<{ names: string[]; items: ThemeInfo[] }>('/themes'),
    themeFile: (name: string, target: 'site' | 'admin') =>
      getText(`/themes/${encodeURIComponent(name)}/${target}.css`),
    plugins: () => get<UserPlugin[]>('/plugins'),
    setupStatus: () => get<SetupStatus>('/setup/status'),
  }
}
