// 插件通知桥:把 notify.send 的返回值渲染成全局通知 / 区域内提示 / 弹出框
import { ElMessageBox, ElNotification } from 'element-plus'

export interface PluginNotice {
  id: number
  level: 'info' | 'success' | 'warning' | 'error'
  title: string
  message: string
}

const LEVELS = ['info', 'success', 'warning', 'error'] as const
const SCOPES = ['global', 'inline', 'modal'] as const

export function usePluginNotify() {
  // 区域内提示:由宿主页面在插件 iframe 附近渲染,可手动关闭
  const inline = ref<PluginNotice[]>([])
  let seq = 0

  const dismiss = (id: number) => {
    inline.value = inline.value.filter((n) => n.id !== id)
  }

  const show = (raw: any) => {
    const p = raw || {}
    const message = String(p.message ?? '').trim()
    if (!message) return
    const level = (LEVELS as readonly string[]).includes(p.level) ? (p.level as PluginNotice['level']) : 'info'
    const title = String(p.title ?? '').trim()
    const scope = (SCOPES as readonly string[]).includes(p.scope) ? (p.scope as (typeof SCOPES)[number]) : 'global'
    const duration = Number(p.duration) || 3000

    if (scope === 'modal') {
      ElMessageBox.alert(message, title || '通知', {
        type: level,
        confirmButtonText: '知道了',
      }).catch(() => {})
      return
    }
    if (scope === 'inline') {
      seq += 1
      inline.value.push({ id: seq, level, title, message })
      return
    }
    ElNotification({ title: title || undefined, message, type: level, duration })
  }

  return { inline, show, dismiss }
}
