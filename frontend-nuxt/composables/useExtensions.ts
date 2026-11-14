// 追踪当前启用的插件 slug 列表(公开 + 后台都用)。
// 后台调用 refresh() 时会带上管理端数据;公开侧只拿启用 slug 列表。
import type { Extension, MediaContributions } from '~/types'

let publicInFlight: Promise<void> | null = null

export function useExtensions() {
  const base = (useRuntimeConfig().public.apiBase || '/api/v1').replace(/\/+$/, '')
  const enabled = useState<string[]>('fanke_ext_enabled', () => [])
  const loaded = useState<boolean>('fanke_ext_loaded', () => false)
  const contributions = useState<MediaContributions | null>('fanke_media_contributions', () => null)
  const contributionsLoaded = useState<boolean>('fanke_media_contributions_loaded', () => false)

  // 公开侧:仅拿启用 slug 列表(无需登录)。
  function refreshPublic(): Promise<void> {
    if (publicInFlight) return publicInFlight
    publicInFlight = (async () => {
      try {
        const res = await fetch(`${base}/ext`)
        if (!res.ok) return
        const j = await res.json()
        if (!Array.isArray(j?.enabled)) return
        enabled.value = j.enabled
        loaded.value = true
      } catch {
        // 请求失败时保留原状态,不能将未知状态当作插件关闭。
      } finally {
        publicInFlight = null
      }
    })()
    return publicInFlight
  }

  function ensurePublic(): Promise<void> {
    return publicInFlight || (loaded.value ? Promise.resolve() : refreshPublic())
  }

  function isEnabled(slug: string) {
    return enabled.value.includes(slug)
  }

  async function refreshContributions() {
    try {
      const { api } = useAuth()
      const res = await api<MediaContributions>('/admin/extensions/contributions')
      contributions.value = {
        settings: Array.isArray(res?.settings) ? res.settings : [],
        upload_menu: Array.isArray(res?.upload_menu) ? res.upload_menu : [],
        image_processing: res?.image_processing === true,
        s3_storage: res?.s3_storage === true,
      }
    } catch {
      contributions.value = { settings: [], upload_menu: [], image_processing: false, s3_storage: false }
    } finally {
      contributionsLoaded.value = true
    }
  }

  // 后台:完整插件列表。
  async function listAdmin(): Promise<Extension[]> {
    const { api } = useAuth()
    const res = await api<{ items: Extension[] }>(`/admin/extensions`)
    return res.items || []
  }

  return { enabled, loaded, refreshPublic, ensurePublic, isEnabled, listAdmin, contributions, contributionsLoaded, refreshContributions }
}
