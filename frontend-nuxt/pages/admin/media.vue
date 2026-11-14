<script setup lang="ts">
// 媒体库:上传(XHR 带进度)+ 网格管理(网格逻辑复用 MediaLibrary)
definePageMeta({ layout: 'admin', middleware: 'auth' })
import { mediaUploadHint } from '~/utils/upload'

const { options, target, ensureContributions, setTarget, uploadWithRetry } = useMediaUpload()
const { push } = useToast()

const fileEl = ref<HTMLInputElement>()
const fileName = ref('')
const storage = target
const uploading = ref(false)
const progVisible = ref(false)
const progNum = ref(0)
const lib = ref<{ reload: () => void }>()

const onPick = () => {
  fileName.value = fileEl.value?.files?.[0]?.name ?? ''
}
onMounted(() => { void ensureContributions() })

const upload = async () => {
  const file = fileEl.value?.files?.[0]
  if (!file) {
    push('请先选择文件', false)
    return
  }
  uploading.value = true
  progVisible.value = true
  progNum.value = 0
  try {
    const out = await uploadWithRetry(file, (percent) => { progNum.value = percent })
    progNum.value = 100
    push(`上传成功:${out.filename}`, true)
    if (fileEl.value) fileEl.value.value = ''
    fileName.value = ''
    lib.value?.reload()
  } catch (e: any) {
    push(e.message || '上传失败', false)
  } finally {
    uploading.value = false
    setTimeout(() => (progVisible.value = false), 1200)
  }
}

useHead({ title: '媒体 · fanke 后台' })
</script>

<template>
  <section class="panel card">
    <div class="toolbar">
      <h2 style="margin: 0">媒体库</h2>
      <input
        ref="fileEl"
        type="file"
        hidden
        accept=".jpg,.jpeg,.png,.gif,.webp,.svg,.avif,.mp4,.webm,.mov,.pdf,.zip,.txt,.md,.csv"
        @change="onPick"
      />
      <el-button @click="fileEl?.click()">选择文件</el-button>
      <span v-if="fileName" class="pick-name" :title="fileName">{{ fileName }}</span>
      <el-select v-model="storage" style="width: 150px" @change="setTarget">
        <el-option v-for="item in options" :key="item.value" :value="item.value" :label="item.label" />
      </el-select>
      <el-button type="primary" :loading="uploading" :disabled="!fileName || uploading" @click="upload">上传</el-button>
    </div>
    <el-progress v-if="progVisible" :percentage="progNum" :status="progNum >= 100 ? 'success' : undefined" style="margin: 10px 0 2px" />
    <p class="muted">{{ mediaUploadHint }}</p>
    <MediaLibrary ref="lib" mode="manage" />
  </section>
</template>

<style scoped>
.pick-name {
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
  color: var(--muted);
}
</style>
