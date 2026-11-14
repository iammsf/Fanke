// 安装状态(10s 缓存):未配置/断库/空库/后端不可达 → 引导到 /setup
import type { SetupStatus } from '~/types'

export function useSetup() {
  const needs = useState<boolean>('setup_needs', () => false)
  const checkedAt = useState<number>('setup_checked_at', () => 0)

  const check = async (force = false): Promise<boolean> => {
    if (!force && checkedAt.value && Date.now() - checkedAt.value < 10_000) return needs.value
    const api = usePublicApi()
    const s: SetupStatus | null = await api.setupStatus()
    // 后端不可达也进向导(向导页内给出"无法连接后端"提示)
    needs.value = !s || s.state !== 'ok' || !!s.needs_setup
    checkedAt.value = Date.now()
    return needs.value
  }

  return { needs, check }
}
