<script setup lang="ts">
// 文章编辑器(新建/编辑共用同一个组件):
//   /admin/posts/new       → <PostEditor />
//   /admin/posts/[id]      → <PostEditor :post-id="id" />
import { ElMessage } from 'element-plus'
import { validateMediaUpload } from '~/utils/upload'
import type { AdminPost, MediaItem, TermRef } from '~/types'

const props = defineProps<{ postId?: number }>()

const { api } = useAuth()
const { options, ensureContributions, setTarget, uploadWithRetry } = useMediaUpload()
const { push } = useToast()
const { run } = useBusy() // 与 DB/存储交互期间的全屏加载遮罩(兼防连点)
const router = useRouter()

// 插件节点属性面板：schema 由插件 editor.ts 提供，这里只负责通用渲染
import type { PanelSection } from '~/composables/useEditorRegistry'

const nodeDlgOpen = ref(false)
const nodeTag = ref('')
const nodeLabel = ref('')
const nodeSections = ref<PanelSection[]>([])
const nodeValues = reactive<Record<string, any>>({})
const nodeUploading = ref(false)
const nodeFile = ref<File | null>(null)
const nodeFileEl = ref<HTMLInputElement>()

const resetNodeValues = () => {
  for (const k of Object.keys(nodeValues)) delete nodeValues[k]
}

// 全页只保留一个媒体选择对话框 + 一个 MediaLibrary 实例,按 target 复用。
type MediaTarget = '' | 'cover' | 'insert-plain' | 'node'
const mediaTarget = ref<MediaTarget>('')
const pendingNodeTag = ref('')
const mediaDlgOpen = computed({
  get: () => !!mediaTarget.value,
  set: (v: boolean) => {
    if (!v) mediaTarget.value = ''
  },
})
const mediaDlgTitle = computed(() => {
  switch (mediaTarget.value) {
    case 'cover': return '选择封面'
    case 'insert-plain': return '插入普通图片'
    case 'node': return `插入${nodeLabelOf(pendingNodeTag.value)}`
    default: return '媒体库'
  }
})
const nodeLabelOf = (tag: string) => nodeLabel.value || tag
const openMedia = (t: Exclude<MediaTarget, ''>) => {
  mediaTarget.value = t
}
const onEditorMedia = (_mode: 'plain') => openMedia('insert-plain')
const onPickerNode = (tag: string) => {
  pendingNodeTag.value = tag
  openMedia('node')
}
const onMediaPick = (m: MediaItem) => {
  if (mediaTarget.value === 'cover') pickCover(m)
  else if (mediaTarget.value === 'insert-plain') insertPlainMedia(m)
  else if (mediaTarget.value === 'node') {
    richEditor.value?.insertNode?.(pendingNodeTag.value, m)
    mediaTarget.value = ''
  }
}

// ---------- 表单状态 ----------
const form = reactive({
  title: '',
  slug: '',
  cats: '',
  tags: '',
  summary: '',
  status: 'draft' as 'draft' | 'published',
  type: 'standard' as 'standard' | 'moment',
  pubdate: '',
  allow_comments: true,
  is_public: true,
  content_html: '',
})
const richEditor = ref<any>(null)
const insertPlainMedia = (m: MediaItem) => {
  richEditor.value?.insertPlainImage?.(m)
  mediaTarget.value = ''
}
const coverId = ref<number | null>(null)
const coverName = ref('未选择')
const coverThumb = ref('')

const loading = ref(!!props.postId)
const loadError = ref<any>(null)
const saving = ref(false)
const slugTouched = ref(false)

// slug 未手动改过时跟随标题
watch(
  () => form.title,
  (t) => {
    if (!slugTouched.value) form.slug = suggestSlug(t)
  },
)
watch(
  () => form.slug,
  () => {
    slugTouched.value = true
  },
)

// 插件面板回填标题/摘要(面板逻辑在插件包内);允许回填空串用于撤销
const applyPluginSuggestions = (suggestions: { title?: string; summary?: string }) => {
  if (typeof suggestions.title === 'string') form.title = suggestions.title
  if (typeof suggestions.summary === 'string') form.summary = suggestions.summary
}

// 编辑器选中文本:传给插件面板,供「AI 润色」对选中段落操作
const editorSelection = ref('')
let selectionTimer: ReturnType<typeof setTimeout> | null = null
const onEditorSelection = (text: string) => {
  if (selectionTimer) clearTimeout(selectionTimer)
  selectionTimer = setTimeout(() => { editorSelection.value = text }, 150)
}
// 润色结果替换选中内容;撤销走编辑器自带历史栈
const applyPolishText = (text: string) => richEditor.value?.insertSelectionText?.(text)
const undoEditorChange = () => richEditor.value?.undo?.()

// 编辑器双击插件节点 → 用插件给的 schema 打开通用面板
const openNodeDialog = (p: { tag: string; label: string; sections: PanelSection[]; values: Record<string, any> }) => {
  nodeTag.value = p.tag
  nodeLabel.value = p.label
  nodeSections.value = p.sections || []
  resetNodeValues()
  Object.assign(nodeValues, p.values || {})
  nodeFile.value = null
  nodeDlgOpen.value = true
}

// 封面缩略图预览
const coverViewerOpen = ref(false)
const openCoverViewer = () => {
  if (coverThumb.value) coverViewerOpen.value = true
}

const applyNode = () => {
  richEditor.value?.applyNodeSettings?.({ ...nodeValues })
  nodeDlgOpen.value = false
  push('已更新', true)
}

// 面板里的上传：把地址写回 src 字段
const nodeUpload = async () => {
  if (!nodeFile.value) return
  try { validateMediaUpload(nodeFile.value, nodeFile.value.name) } catch (e: any) { push(e.message, false); return }
  nodeUploading.value = true
  try {
    const j = await run('上传中…', () => uploadWithRetry(nodeFile.value!))
    nodeValues.src = j.url
    push('已上传，记得点保存', true)
  } catch (e: any) {
    push(e.message || '上传失败', false)
  } finally {
    nodeUploading.value = false
  }
}

// ---------- 封面 ----------
const pickCover = (m: MediaItem) => {
  coverId.value = m.id
  coverName.value = m.filename
  coverThumb.value = m.mime.startsWith('image/') ? m.url : ''
  mediaTarget.value = ''
  push(`已选择封面 #${m.id}`, true)
}
const coverFileEl = ref<HTMLInputElement>()
const coverUploading = ref(false)
const uploadCover = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file || coverUploading.value) return
  coverUploading.value = true
  try {
    if (!file.type.startsWith('image/')) throw new Error('请选择图片文件')
    validateMediaUpload(file, file.name)
    const media = await uploadWithRetry(file)
    pickCover(media)
    if (media.warning) ElMessage.warning(media.warning)
  } catch (e: any) {
    ElMessage.error(e.message || '封面上传失败')
  } finally {
    coverUploading.value = false
    input.value = ''
  }
}
const clearCover = () => {
  coverId.value = null
  coverName.value = '未选择'
  coverThumb.value = ''
}

// ---------- 保存 ----------
const splitTerms = (v: string): TermRef[] => {
  const seen = new Set<string>()
  return v
    .split(/[,，]/)
    .map((s) => s.trim())
    .filter((s) => s && !seen.has(s) && seen.add(s))
    .map((name) => ({ name, slug: name }))
}

const pad2 = (n: number) => String(n).padStart(2, '0')
const toLocalInput = (iso: string | null): string => {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}T${pad2(d.getHours())}:${pad2(d.getMinutes())}`
}

const save = async (status: 'draft' | 'published') => {
  const slug = form.slug.trim() || suggestSlug(form.title)
  form.slug = slug
  if (!form.title.trim()) {
    push('标题不能为空', false)
    return
  }
  if (!validSlug(slug)) {
    push('slug 无效:需非空、≤200、仅允许中文(含标点)/字母数字/% _ - . ~(且不含 ..)', false)
    return
  }
  saving.value = true
  try {
    await richEditor.value?.uploadImages()
    form.content_html = richEditor.value?.getContent() ?? form.content_html
  } catch (e: any) {
    push(e.message || '图片上传失败，保存已取消', false)
    saving.value = false
    return
  }
  const body = {
    slug,
    title: form.title.trim(),
    summary: form.summary.trim(),
    content_html: form.content_html,
    status,
    type: form.type,
    cover_media_id: coverId.value,
    allow_comments: form.allow_comments,
    is_public: form.is_public,
    published_at: form.pubdate ? new Date(form.pubdate).toISOString() : null,
    categories: splitTerms(form.cats),
    tags: splitTerms(form.tags),
  }
  try {
    if (props.postId) {
      await run(status === 'published' ? '发布中…' : '保存中…', () =>
        api(`/admin/posts/${props.postId}`, { method: 'PUT', body: JSON.stringify(body) }),
      )
      push(status === 'published' ? '已保存并发布' : '已保存', true)
      if (status === 'published') form.status = 'published'
    } else {
      const p = await run('创建中…', () => api<AdminPost>('/admin/posts', { method: 'POST', body: JSON.stringify(body) }))
      push('已创建', true)
      router.replace(`/admin/posts/${p.id}`)
    }
  } catch (e: any) {
    if (e.status === 409) push('slug 已存在,请修改后再保存', false)
    else push(e.message || '保存失败', false)
  } finally {
    saving.value = false
  }
}

// ---------- 编辑模式:载入既有文章 ----------
const loadPost = async () => {
  if (!props.postId) return
  loading.value = true
  loadError.value = null
  try {
    const p = await api<AdminPost>(`/admin/posts/${props.postId}`)
    form.title = p.title || ''
    form.slug = p.slug || ''
    slugTouched.value = true
    form.summary = p.summary || ''
    // 历史文章优先直接载入已保存 HTML，避免 Markdown 往返吞掉图片说明属性。
    form.content_html = p.content_html || ''
    form.status = p.status === 'published' ? 'published' : 'draft'
    form.type = p.type === 'moment' ? 'moment' : 'standard'
    form.allow_comments = !!p.allow_comments
    form.is_public = p.is_public !== false
    form.pubdate = toLocalInput(p.published_at)
    form.cats = (p.terms ?? []).filter((t) => t.type === 'category').map((t) => t.name).join(', ')
    form.tags = (p.terms ?? []).filter((t) => t.type === 'tag').map((t) => t.name).join(', ')
    coverId.value = p.cover_media_id ?? null
    if (p.cover_url) {
      coverThumb.value = p.cover_url
      coverName.value = `封面 #${p.cover_media_id}`
    }
  } catch (e: any) {
    loadError.value = e
  } finally {
    loading.value = false
  }
}
onMounted(() => {
  void ensureContributions()
  loadPost()
})
</script>

<template>
  <el-skeleton v-if="loading" :rows="8" animated />
  <ErrorBox v-else-if="loadError" :error="loadError" retry @retry="loadPost" />

  <el-form v-else class="panel card post-editor-form" label-position="top" autocomplete="off" @submit.prevent>
    <div class="toolbar">
      <h1 style="margin: 0; font-size: 20px">{{ postId ? `编辑文章 #${postId}` : '新文章' }}</h1>
      <div style="margin-left: auto; display: flex; gap: 8px; flex-wrap: wrap; align-items: center">
        <PluginPanelHost
          :post-id="postId"
          :title="form.title"
          :summary="form.summary"
          :content="form.content_html"
          :selection="editorSelection"
          @apply="applyPluginSuggestions"
          @apply-content="applyPolishText"
          @undo-content="undoEditorChange"
        />
        <el-button @click="navigateTo('/admin/posts')">返回列表</el-button>
        <el-button :disabled="saving" @click="save('draft')">保存草稿</el-button>
        <el-button type="primary" :disabled="saving" @click="save('published')">保存并发布</el-button>
      </div>
    </div>

    <div class="ed-wrap">
      <div class="ed-cols2">
        <el-form-item label="标题 *">
          <el-input v-model="form.title" maxlength="200" required placeholder="文章标题" />
        </el-form-item>
        <el-form-item label="Slug *">
          <el-input v-model="form.slug" maxlength="200" placeholder="留空将根据标题自动生成" />
          <div class="hint" style="font-size:12px;color:var(--muted);margin-top:4px">允许中文(含标点)、字母数字与 % _ - . ~,≤200;重复将提示 409</div>
        </el-form-item>
      </div>

      <div class="ed-cols3">
        <el-form-item label="分类(逗号分隔)">
          <el-input v-model="form.cats" placeholder="技术, 生活" />
        </el-form-item>
        <el-form-item label="标签(逗号分隔)">
          <el-input v-model="form.tags" placeholder="Go, Vue" />
        </el-form-item>
        <el-form-item label="文章类型">
          <el-select v-model="form.type">
            <el-option
              v-for="o in [
                { value: 'standard', label: '标准文章' },
                { value: 'moment', label: '随记' },
              ]"
              :key="o.value"
              :label="o.label"
              :value="o.value"
              :disabled="o.disabled"
            />
          </el-select>
          <div class="hint" style="font-size:12px;color:var(--muted);margin-top:4px">随记:简单几句话</div>
        </el-form-item>
      </div>

      <el-form-item label="摘要">
        <el-input v-model="form.summary" type="textarea" :rows="2" placeholder="列表页展示的一两句话" />
      </el-form-item>

      <div class="post-meta-row">
        <el-form-item label="状态">
          <el-select v-model="form.status">
            <el-option
              v-for="o in [{ value: 'draft', label: '草稿' }, { value: 'published', label: '已发布' }]"
              :key="o.value"
              :label="o.label"
              :value="o.value"
              :disabled="o.disabled"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="发布时间(选填)">
          <el-input v-model="form.pubdate" type="datetime-local" />
        </el-form-item>
        <el-form-item label="封面图">
          <div class="post-cover-controls">
            <input ref="coverFileEl" type="file" accept="image/*" hidden :disabled="coverUploading" @change="uploadCover" />
            <el-button :disabled="coverUploading" @click="openMedia('cover')">选择</el-button>
            <el-select :model-value="undefined" placeholder="上传目标" style="width: 130px" @change="setTarget">
              <el-option v-for="item in options" :key="item.value" :value="item.value" :label="item.label" />
            </el-select>
            <el-button :loading="coverUploading" @click="coverFileEl?.click()">上传</el-button>
            <el-button :disabled="coverUploading" @click="clearCover">清除</el-button>
            <img
              v-if="coverThumb"
              :src="coverThumb"
              class="cover-thumb"
              alt="封面预览"
              title="点击预览"
              @click="openCoverViewer"
            />
            <span class="muted post-cover-name" :title="coverName">{{ coverName }}</span>
          </div>
        </el-form-item>
        <el-form-item label="公开性与评论">
          <div class="post-options">
            <el-checkbox v-model="form.is_public" title="取消公开则前端不展示">公开</el-checkbox>
            <el-checkbox v-model="form.allow_comments">允许评论</el-checkbox>
          </div>
        </el-form-item>
      </div>

      <el-form-item label="正文（所见即所得）">
        <RichTextEditor
          ref="richEditor"
          v-model="form.content_html"
          @media="onEditorMedia"
          @insert-node="onPickerNode"
          @edit-node="openNodeDialog"
          @selection-change="onEditorSelection"
        />
        <div class="hint" style="font-size:12px;color:var(--muted);margin-top:6px">
          直接在正文区排版，显示效果即发布效果。安装了图片类插件时，工具栏「插入」菜单会自动多出对应条目，双击正文里的插件节点可改属性。
        </div>
      </el-form-item>
    </div>

    <!-- 媒体库(封面 / 插入正文) -->
    <el-dialog
      v-model="mediaDlgOpen"
      :title="mediaDlgTitle"
      width="min(70vw, calc(100vw - 32px))"
      top="10vh"
      class="post-media-dialog"
    >
      <div class="media-slot">
        <MediaLibrary mode="pick" :page-size="21" @pick="onMediaPick" />
      </div>
    </el-dialog>

    <!-- 插件节点属性面板：字段由插件 editor.ts 的 schema 决定，这里通用渲染 -->
    <el-dialog v-model="nodeDlgOpen" :title="`${nodeLabel}设置`" width="min(760px, calc(100vw - 32px))" top="8vh">
      <p v-if="!nodeSections.length" class="muted">该插件没有提供可编辑属性。</p>

      <div v-if="nodeSections.some((s) => s.fields.some((f) => f.key === 'src'))" class="img-src-row">
        <input ref="nodeFileEl" type="file" accept="image/*" hidden @change="(e: Event) => { nodeFile = (e.target as HTMLInputElement).files?.[0] || null }" />
        <el-button @click="nodeFileEl?.click()">选择文件</el-button>
        <span v-if="nodeFile" class="muted img-file-name" :title="nodeFile.name">{{ nodeFile.name }}</span>
        <el-button type="primary" :loading="nodeUploading" :disabled="!nodeFile" @click="nodeUpload">上传</el-button>
      </div>

      <template v-for="(sec, si) in nodeSections" :key="si">
        <h4 v-if="sec.title" class="node-sec-title">{{ sec.title }}</h4>
        <el-form-item v-for="f in sec.fields" :key="f.key" :label="f.label">
          <el-checkbox v-if="f.type === 'checkbox'" v-model="nodeValues[f.key]">{{ f.label }}</el-checkbox>
          <el-select v-else-if="f.type === 'select'" v-model="nodeValues[f.key]" :style="{ width: f.width || '160px' }">
            <el-option v-for="o in f.options || []" :key="o.value" :label="o.label" :value="o.value" />
          </el-select>
          <div v-else-if="f.type === 'color'" class="img-pos-row">
            <el-color-picker v-model="nodeValues[f.key]" size="small" />
            <el-input v-model="nodeValues[f.key]" :placeholder="f.placeholder || '选择或输入颜色'" style="width: 130px" />
          </div>
          <el-input
            v-else
            v-model="nodeValues[f.key]"
            :type="f.type === 'number' ? 'number' : 'text'"
            :placeholder="f.placeholder"
            :style="{ width: f.width || '220px' }"
          />
        </el-form-item>
      </template>

      <template #footer>
        <el-button @click="nodeDlgOpen = false">取消</el-button>
        <el-button type="primary" @click="applyNode">保存</el-button>
      </template>
    </el-dialog>

    <!-- Element Plus 图片查看器:封面缩略图 -->
    <el-image-viewer
      v-if="coverViewerOpen"
      :url-list="[coverThumb]"
      teleported
      hide-on-click-modal
      @close="coverViewerOpen = false"
    />
  </el-form>
</template>

<style scoped>
.post-editor-form .ed-wrap,
.post-editor-form .ed-cols2,
.post-editor-form .ed-cols3 {
  gap: 12px;
}
.post-editor-form :deep(.el-form-item) {
  margin-bottom: 0;
}
.post-editor-form :deep(.el-form-item__label) {
  display: block;
  float: none;
  width: auto;
  height: 20px;
  padding: 0;
  margin: 0 0 6px;
  font-size: 13px;
  line-height: 20px;
  font-weight: 400;
}
.post-editor-form :deep(.el-form-item__content) {
  margin-left: 0;
  min-width: 0;
}
/* 媒体选择对话框(.post-media-dialog)样式已移至 global.css,全局复用 */
.post-editor-form .hint {
  line-height: 20px;
}
.post-editor-form .post-meta-row {
  display: grid;
  grid-template-columns: 120px 220px minmax(280px, 1fr) 210px;
  gap: 12px;
  align-items: start;
}
.post-meta-row > :deep(.el-form-item),
.post-meta-row :deep(.el-form-item__content) {
  min-width: 0;
}
.post-meta-row :deep(.el-form-item__label) {
  height: 20px;
}
.post-meta-row :deep(.el-form-item__content) {
  line-height: 32px;
}
.post-meta-row :deep(.el-input),
.post-meta-row :deep(.el-input__wrapper),
.post-meta-row :deep(.el-select__wrapper),
.post-meta-row :deep(.el-button),
.post-meta-row :deep(.el-checkbox) {
  height: 32px;
  min-height: 32px;
}
.post-editor-form .post-options {
  display: flex;
  flex-wrap: nowrap;
  align-items: center;
  column-gap: 12px;
  height: 32px;
}
.post-editor-form .post-options :deep(.el-checkbox) {
  margin-right: 0;
}
.post-cover-controls {
  display: flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: 8px;
  width: 100%;
  min-width: 0;
  height: 32px;
}
.post-cover-controls :deep(.el-button) {
  flex-shrink: 0;
  margin-left: 0;
  padding: 8px 10px;
}
.post-cover-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.post-cover-controls .cover-thumb {
  width: 72px;
  height: 32px;
  object-fit: contain;
  flex-shrink: 0;
  cursor: zoom-in;
}
.img-src-row {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
}
.img-file-name {
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
}
.img-preview {
  margin-top: 12px;
  min-height: 140px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px dashed var(--border, #e4e7ed);
  border-radius: 8px;
  padding: 10px;
}
.img-preview img {
  max-width: 100%;
  max-height: 320px;
  border-radius: 6px;
}
.img-pos-row {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
}
.media-slot {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
@media (max-width: 1200px) {
  .post-editor-form .post-meta-row {
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  }
}
@media (max-width: 720px) {
  .post-editor-form .post-meta-row {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
