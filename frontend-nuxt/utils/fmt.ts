// 全站通用格式化/校验助手(Nuxt 自动导入)
const pad2 = (n: number) => String(n).padStart(2, '0')

export function fmtDate(iso: string | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`
}

export function fmtDateTime(iso: string | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return `${fmtDate(iso)} ${pad2(d.getHours())}:${pad2(d.getMinutes())}`
}

export function fmtBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return ''
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(2)} MB`
}

export function readingMinutes(text: string): number {
  const cjk = (text.match(/[一-鿿]/g) || []).length
  const words = (text.match(/[a-zA-Z0-9_']+/g) || []).length
  return Math.max(1, Math.round((cjk + words) / 400))
}

// slug 规则(与后端一致:中文及中文标点/字母数字/% _ - . ~/常见符号与 emoji,非空,≤200,不含 ..;
// 标点与 emoji 区间是为了兼容 WordPress 迁移来的历史 slug)
export function validSlug(s: string): boolean {
  return (
    s !== '' &&
    s.length <= 200 &&
    /^[a-zA-Z0-9%_\-.~\u4e00-\u9fff\u3000-\u303f\uff01-\uff65\u2600-\u27bf\u{1f300}-\u{1faff}]+$/u.test(s) &&
    !s.includes('..')
  )
}

export function suggestSlug(s: string): string {
  return s
    .trim()
    .toLowerCase()
    .replace(/[\s/\\]+/g, '-')
    .replace(/[^a-zA-Z0-9%_\-.~一-鿿]/g, '')
    .replace(/-{2,}/g, '-')
    .replace(/^[-.]+|[-.]+$/g, '')
    .slice(0, 200)
}

// 统一错误文案:加载失败必须给出明确原因
export function errText(e: any): string {
  if (e?.status === 0 || e?.name === 'TypeError') return '无法连接服务器,请检查网络或后端服务是否在运行'
  if (e?.status === 503) return '服务暂不可用,请稍后重试'
  return e?.message || '加载失败'
}
