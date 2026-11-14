// 分类/标签术语缓存。
//
// 总览页(/categories、/tags)与详情页共用同一份数据:
// 从总览点进详情时,标题和胶囊可以直接用缓存渲染,
// 不会先塌成骨架屏再长出来。
import type { TermWithCount } from '~/types'

const cache = new Map<string, TermWithCount[]>()

export function useTermCache(type: 'category' | 'tag') {
  const key = type === 'category' ? 'category' : 'tag'
  const terms = useState<TermWithCount[]>(`fanke_terms_${key}`, () => cache.get(key) || [])
  const loaded = useState<boolean>(`fanke_terms_loaded_${key}`, () => cache.has(key))
  const api = usePublicApi()

  /** 拉取术语列表;成功返回列表,失败返回 null(保留已有缓存)。 */
  const ensure = async (force = false): Promise<TermWithCount[] | null> => {
    if (loaded.value && !force) return terms.value
    const res = await api.terms(type)
    if (!res) return loaded.value ? terms.value : null
    terms.value = res
    cache.set(key, res)
    loaded.value = true
    return res
  }

  return { terms, loaded, ensure }
}
