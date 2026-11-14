// 图片焦点(image-focus 插件)前台工具:
// - 批量加载 focus 数据(同一 tick 内的请求自动合并为一次 get_batch 调用)
// - 按容器宽高比挑选最合适的取景框,计算 object-position
// 仅在插件启用时被调用;未启用时 FocusImg 退化为普通 <img>。
import type { FocusData, FocusRect } from '~/types'

const cache = new Map<string, FocusData | null>()
const queue: string[] = []
const queued = new Set<string>()
let scheduled = false

// 响应式 tick:每批结果落地后 +1,触发组件重算
const tick = ref(0)

function schedule(base: string) {
  if (scheduled) return
  scheduled = true
  queueMicrotask(() => {
    scheduled = false
    void flush(base)
  })
}

async function flush(base: string) {
  const urls = queue.splice(0)
  if (!urls.length) return
  try {
    const res = await fetch(`${base}/ext/image-focus/hook/store.get_batch`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ keys: urls }),
    })
    if (!res.ok) return
    const j = await res.json()
    if (j?.ok !== true || !j.data?.items || typeof j.data.items !== 'object' || Array.isArray(j.data.items)) return
    const items = j.data.items as Record<string, FocusData>
    for (const u of urls) cache.set(u, items[u] || null)
    tick.value++
  } catch {
    // 失败不缓存为无焦点数据,后续显式请求仍可重试。
  } finally {
    for (const u of urls) queued.delete(u)
  }
}

export function useImageFocus() {
  const base = (useRuntimeConfig().public.apiBase || '/api/v1').replace(/\/+$/, '')
  // 请求某 url 的焦点数据;返回响应式 computed(数据未到时为 null)
  function focusOf(url: string) {
    if (!cache.has(url) && !queued.has(url)) {
      queued.add(url)
      queue.push(url)
      schedule(base)
    }
    return computed(() => {
      tick.value // 建立依赖
      return cache.get(url) ?? null
    })
  }

  // 该 url 的焦点数据是否已落地(含"确认无数据")。响应式:批次落地后会重新求值。
  // 供 FocusImg 做显示门控:焦点数据先到位,图片再显示,避免默认位置画出来后再跳一下。
  function isResolved(url: string): boolean {
    tick.value // 建立响应式依赖
    return cache.has(url)
  }

  // 批量预热(列表页可选优化)
  function prefetch(urls: string[]) {
    for (const u of urls) {
      if (!cache.has(u) && !queued.has(u)) {
        queued.add(u)
        queue.push(u)
      }
    }
    schedule(base)
  }

  function invalidate(url?: string) {
    if (url) cache.delete(url)
    else cache.clear()
    tick.value++
  }

  return { focusOf, isResolved, prefetch, invalidate }
}

// rectKeyFor 按容器宽高比选取景框:近似 1:1 / 16:9 / 9:16,否则用中心点。
export function rectKeyFor(aspect: number): 'square' | 'wide' | 'tall' | null {
  if (aspect >= 0.85 && aspect <= 1.18) return 'square'
  if (aspect >= 1.55 && aspect <= 2.05) return 'wide'
  if (aspect >= 0.48 && aspect <= 0.68) return 'tall'
  return null
}

// 框无效判定:与内置默认框基本一致(扫描批量写入的占位值),或几乎覆盖整图。
// 这类框不携带位置信息,用它的中心只会得到≈正中心,反而把用户设的焦点(cx/cy)顶掉。
const isDefaultRect = (key: 'square' | 'wide' | 'tall', r: FocusRect) => {
  const d = defaultFocus()[key]
  const t = 0.01
  return Math.abs(r.x - d.x) < t && Math.abs(r.y - d.y) < t && Math.abs(r.w - d.w) < t && Math.abs(r.h - d.h) < t
}
const isFullFrame = (r: FocusRect) => r.w * r.h >= 0.98

// objectPositionFor 计算 CSS object-position。
// 精确版:需要图片实际宽高比。cover 模式下 object-position 百分比不是图片像素的线性映射,
// 必须按 (目标点 - 容器中心) / 可移动范围 反推,否则框中心会对不准。
// 框有效 → 用框中心;框无效/无匹配 → 用焦点中心点(cx, cy)。
export function objectPositionFor(f: FocusData, containerAspect: number, imgAspect: number): string {
  const k = rectKeyFor(containerAspect)
  const rect = k ? f[k] : null
  const useRect = rect && k && !isDefaultRect(k, rect) && !isFullFrame(rect)
  const cx = useRect ? rect.x + rect.w / 2 : f.cx
  const cy = useRect ? rect.y + rect.h / 2 : f.cy

  // cover 缩放:宽高比大的方向溢出
  let r: number
  let overflowX: boolean
  if (imgAspect >= containerAspect) {
    r = imgAspect / containerAspect
    overflowX = true
  } else {
    r = containerAspect / imgAspect
    overflowX = false
  }

  // p = (0.5 - norm * r) / (1 - r),r=1 时该方向刚好填满,p 不影响
  const calc = (norm: number, overflow: boolean) => {
    if (!overflow) return 50
    const p = (0.5 - norm * r) / (1 - r)
    return Math.min(100, Math.max(0, p * 100))
  }

  return `${calc(cx, overflowX).toFixed(2)}% ${calc(cy, !overflowX).toFixed(2)}%`
}

// defaultFocus 与后端 DefaultFocus 保持一致。
export function defaultFocus(): FocusData {
  return {
    cx: 0.5,
    cy: 0.5,
    square: { x: 0.25, y: 0.25, w: 0.5, h: 0.5 },
    wide: { x: 0.1, y: 0.21875, w: 0.8, h: 0.45 },
    tall: { x: 0.275, y: 0.1, w: 0.45, h: 0.8 },
  }
}
