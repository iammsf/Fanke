// 轻提示:统一走 Element Plus ElMessage(顶部居中,2.8s 自动消失)
// 显式 import 函数与样式:程序化调用的组件 @element-plus/nuxt 的按需样式不一定覆盖,
// 显式引入保证样式必定打包进来。
import { ElMessage } from 'element-plus'
import 'element-plus/es/components/message/style/css'

export function useToast() {
  const push = (msg: string, ok = true) => {
    ElMessage({ message: msg, type: ok ? 'success' : 'error', duration: 2800 })
  }
  return { push }
}
