<script setup lang="ts">
// 通用编辑器插件面板宿主:后端只告诉有哪些插件声明了 addEditorPanel,
// 具体 UI 与逻辑全在插件 index.html?panel=1 内,通过 postMessage 与宿主通信。
import type { EditorPanel } from '~/composables/useEditorPanels'

const props = defineProps<{
  title: string
  summary: string
  content: string
  selection?: string
  postId?: number
}>()

const emit = defineEmits<{
  apply: [payload: { title?: string; summary?: string }]
  'apply-content': [text: string]
  'undo-content': []
}>()

const { api, apiBase } = useAuth()
const { push } = useToast()
const { run } = useBusy()
const { panels, load } = useEditorPanels()
// 插件 notify.send:全局通知/区域内提示/弹出框
const { inline: notices, show: showNotice, dismiss: dismissNotice } = usePluginNotify()

const active = ref<EditorPanel | null>(null)
const open = ref(false)
const iframeEl = ref<HTMLIFrameElement>()
const WRITE_HOOKS = new Set(['store.ensure_table', 'store.save', 'store.import', 'store.dedup', 'store.delete'])

onMounted(() => { void load() })

const iframeSrc = computed(() => {
  if (!active.value) return ''
  return `${apiBase}/ext/${active.value.plugin_slug}/page?panel=1`
})

const openPanel = (panel: EditorPanel) => {
  active.value = panel
  open.value = true
}

const sendContent = () => {
  const win = iframeEl.value?.contentWindow
  if (!win) return
  win.postMessage({
    type: 'fanke:editor:content',
    payload: {
      title: props.title,
      summary: props.summary,
      content: props.content,
      selection: props.selection || '',
      post_id: props.postId ?? null,
    },
  }, location.origin)
}

watch([() => props.title, () => props.summary, () => props.content, () => props.selection], sendContent)

const onMessage = async (ev: MessageEvent) => {
  if (ev.origin !== location.origin) return
  if (!iframeEl.value || ev.source !== iframeEl.value.contentWindow) return
  const d: any = ev.data
  if (!d || typeof d !== 'object' || !active.value) return

  if (d.type === 'fanke:hook') {
    const { id, hook, payload } = d
    let ok = true
    let data: any = null
    let errorMsg = ''
    try {
      const call = () => api<{ ok: boolean; data: any }>(
        `/admin/extensions/${active.value!.plugin_slug}/hook/${encodeURIComponent(hook)}`,
        { method: 'POST', body: JSON.stringify(payload ?? {}) },
      )
      const res = WRITE_HOOKS.has(hook) ? await run('保存中…', call) : await call()
      data = res.data
      // 通知类 hook:由宿主渲染(后端只做参数校验与透传)
      if (hook === 'notify.send') showNotice(data || payload)
    } catch (e: any) {
      ok = false
      errorMsg = e.message || 'hook 调用失败'
    }
    iframeEl.value.contentWindow?.postMessage({ type: 'fanke:hook:result', id, ok, data, error: errorMsg }, location.origin)
    return
  }

  if (d.type === 'fanke:editor:apply') {
    const p = d.payload || {}
    // 插件撤销时允许回填空串(allow_empty),普通应用仍要求非空
    const allowEmpty = p.allow_empty === true
    const out: { title?: string; summary?: string } = {}
    for (const key of ['title', 'summary'] as const) {
      if (typeof p[key] !== 'string') continue
      const v = p[key].trim()
      if (v || allowEmpty) out[key] = v
    }
    if (out.title !== undefined || out.summary !== undefined) {
      emit('apply', out)
      push('插件建议已同步到编辑器,请检查后保存', true)
    }
    // 正文润色:把选中的文本替换为插件返回的纯文本
    if (typeof p.content_text === 'string' && p.content_text.trim()) {
      emit('apply-content', p.content_text.trim())
      push('已用润色结果替换选中内容,可保存或撤销', true)
    }
    return
  }

  if (d.type === 'fanke:editor:undo') {
    emit('undo-content')
    push('已撤销编辑器中的上一处改动', true)
    return
  }

  if (d.type === 'fanke:emit') {
    if (d.ev === 'toast' && d.detail?.message) push(String(d.detail.message), d.detail.ok !== false)
  }
}
onMounted(() => window.addEventListener('message', onMessage))
onBeforeUnmount(() => window.removeEventListener('message', onMessage))
</script>

<template>
  <div v-if="panels.length" class="plugin-panels">
    <el-button
      v-for="panel in panels"
      :key="panel.plugin_slug"
      @click="openPanel(panel)"
    >
      {{ panel.name }}
    </el-button>

    <el-drawer v-model="open" :title="active?.name || '插件工具'" direction="rtl" size="min(560px, 100vw)">
      <div v-if="notices.length" class="plugin-notices">
        <el-alert
          v-for="n in notices"
          :key="n.id"
          :title="n.title || undefined"
          :description="n.message"
          :type="n.level"
          show-icon
          closable
          @close="dismissNotice(n.id)"
        />
      </div>
      <iframe
        v-if="active"
        ref="iframeEl"
        class="plugin-frame"
        :src="iframeSrc"
        @load="sendContent"
      />
    </el-drawer>
  </div>
</template>

<style scoped>
.plugin-panels { display: inline-flex; gap: 8px; flex-wrap: wrap; }
.plugin-notices { display: flex; flex-direction: column; gap: 8px; margin-bottom: 10px; }
.plugin-frame {
  width: 100%;
  height: calc(100vh - 140px);
  border: 0;
  display: block;
  background: transparent;
}
</style>
