// 站点设置:全局共享,成功后缓存;布局与页面共用
import type { SiteSettings } from '~/types'

const pendingLoads = new WeakMap<object, Promise<SiteSettings | null>>()

export function useSettings() {
  const nuxtApp = useNuxtApp()
  const settings = useState<SiteSettings | null>('site_settings', () => null)
  const loaded = useState<boolean>('site_settings_loaded', () => false)

  const load = async (force = false): Promise<SiteSettings | null> => {
    const pending = pendingLoads.get(nuxtApp)
    if (pending) return pending
    if (loaded.value && !force) return settings.value
    const api = usePublicApi()
    const request = api.settings().then((result) => {
      if (result !== null) {
        settings.value = result
        loaded.value = true
      }
      return result
    }).finally(() => {
      pendingLoads.delete(nuxtApp)
    })
    pendingLoads.set(nuxtApp, request)
    return request
  }

  const site = computed(() => settings.value?.site ?? {})
  const siteTitle = computed(() => site.value.title || 'fanke')

  return { settings, site, siteTitle, load }
}
