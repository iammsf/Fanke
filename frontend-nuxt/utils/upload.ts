export const mediaUploadHint = '图片 ≤ 100MiB，视频（mp4/webm/mov）≤ 1GiB，其他附件 ≤ 100MiB；重复内容自动去重。实际可用大小还受外层代理限制。'

export function validateMediaUpload(file: Blob, filename = '') {
  const ext = filename.toLowerCase().split('.').pop()
  const video = ['mp4', 'webm', 'mov'].includes(ext || '')
  const limit = video ? 1024 * 1024 * 1024 : 100 * 1024 * 1024
  if (file.size > limit) throw new Error(video ? '视频最大 1GiB' : '图片及其他附件最大 100MiB')
}
