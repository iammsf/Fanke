import type { MediaItem, UploadMenuContribution } from '~/types'
import { validateMediaUpload } from '~/utils/upload'

export function useMediaUpload() {
  const { getToken, tryRefresh, gotoLogin, apiBase } = useAuth()
  const { contributions, contributionsLoaded, refreshContributions } = useExtensions()
  const target = useState<string>('fanke_media_upload_target', () => 'local')
  const menu = computed<UploadMenuContribution[]>(() => (contributions.value?.upload_menu || [])
    .filter((item) => item.storage === 's3' || /^s3:[^\s:]+$/.test(item.storage))
    .slice().sort((a, b) => Number(!!b.is_default) - Number(!!a.is_default)))
  const options = computed(() => [{ value: 'local', label: '本地' }, ...menu.value.map((item) => ({ value: item.storage, label: item.label }))])
  const normalizeTarget = () => {
    if (target.value === 's3' && !menu.value.some((item) => item.storage === 's3')) {
      target.value = menu.value[0]?.storage || 'local'
    }
    if (target.value !== 'local' && !menu.value.some((item) => item.storage === target.value)) target.value = 'local'
  }
  const ensureContributions = async () => {
    if (!contributionsLoaded.value) await refreshContributions()
    normalizeTarget()
  }
  watch(menu, () => { if (contributionsLoaded.value) normalizeTarget() })
  const setTarget = (value: string) => {
    target.value = value
    normalizeTarget()
  }

  function upload(file: File, onProgress?: (percent: number) => void): Promise<MediaItem> {
    return new Promise((resolve, reject) => {
      validateMediaUpload(file, file.name)
      const xhr = new XMLHttpRequest()
      const fd = new FormData()
      fd.append('file', file, file.name)
      normalizeTarget()
      if (target.value !== 'local') fd.append('storage', target.value)
      xhr.open('POST', `${apiBase}/admin/media`)
      const token = getToken()
      if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`)
      xhr.withCredentials = true
      xhr.upload.onprogress = (e) => { if (e.lengthComputable) onProgress?.(Math.round((e.loaded / e.total) * 100)) }
      xhr.onload = () => {
        let body: any = null
        try { body = JSON.parse(xhr.responseText) } catch { /* ignore */ }
        if (xhr.status >= 200 && xhr.status < 300) resolve(body as MediaItem)
        else reject(Object.assign(new Error(body?.error || `上传失败(${xhr.status})`), { status: xhr.status }))
      }
      xhr.onerror = () => reject(new Error('网络错误'))
      xhr.send(fd)
    })
  }

  async function uploadWithRetry(file: File, onProgress?: (percent: number) => void) {
    try { return await upload(file, onProgress) }
    catch (e: any) {
      if (e.status === 401 && (await tryRefresh())) return upload(file, onProgress)
      if (e.status === 401) gotoLogin()
      throw e
    }
  }

  return { target, menu, options, ensureContributions, setTarget, uploadWithRetry }
}
