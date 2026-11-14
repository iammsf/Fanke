// 全屏加载遮罩:凡与数据库 / R2 存储桶交互的操作(上传、批量转移、增删改、装插件等)
// 都套上本遮罩。除给用户明确反馈外,全屏 mask 同时挡住重复点击,避免过快连续操作引发错误。
// 模块级单例 + 引用计数:多处并发操作只维持一个遮罩,嵌套调用不会提前关闭。
import { ElLoading } from 'element-plus'
import 'element-plus/es/components/loading/style/css'

let inst: { close: () => void } | null = null
let depth = 0

export function useBusy() {
  /** 显示遮罩(可重复调用,计数累计) */
  const open = (text = '处理中…') => {
    depth++
    if (!inst) inst = ElLoading.service({ fullscreen: true, lock: true, text, background: 'rgba(0, 0, 0, 0.45)' })
  }

  /** 关掉自己那次;计数归零才真正关闭遮罩 */
  const close = () => {
    if (--depth <= 0) {
      depth = 0
      inst?.close()
      inst = null
    }
  }

  /** 包裹一次交互:自动配对 open/close,异常路径也保证关闭 */
  const run = async <T>(text: string, fn: () => Promise<T>): Promise<T> => {
    open(text)
    try {
      return await fn()
    } finally {
      close()
    }
  }

  return { open, close, run }
}
