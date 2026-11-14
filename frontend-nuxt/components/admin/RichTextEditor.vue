<script setup lang="ts">
import Editor from '@tinymce/tinymce-vue'
import type { MediaItem } from '~/types'
import type { NodeRegistration, PanelSection } from '~/composables/useEditorRegistry'

const props = defineProps<{ modelValue: string }>()
const emit = defineEmits<{
  'update:modelValue': [value: string]
  /** 插入普通 <img> */
  media: [mode: 'plain']
  /** 插入插件节点（tag），父组件弹出媒体库 */
  'insert-node': [tag: string]
  'selection-change': [text: string]
  /** 双击插件节点：带上插件提供的面板 schema 与当前值 */
  'edit-node': [payload: { tag: string; label: string; sections: PanelSection[]; values: Record<string, any> }]
}>()

const { ensureContributions, uploadWithRetry } = useMediaUpload()
const { nodes, ensureLoaded } = useEditorRegistry()
const editor = ref<any>(null)
const ready = ref(false)
const value = ref('')
let disposed = false
let pendingNodeEl: HTMLElement | null = null
let pendingTag = ''
let lastEmitted = ''
let userEdited = false

onBeforeUnmount(() => {
  disposed = true
})

const esc = (s: string) =>
  (s || '').replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;')

const attrMap = (el: Element): Record<string, string> => {
  const out: Record<string, string> = {}
  for (const a of Array.from(el.attributes)) {
    if (a.name === 'data-fanke-node') continue
    out[a.name] = a.value
  }
  return out
}

const fragment = (html: string): DocumentFragment => {
  const t = document.createElement('template')
  t.innerHTML = html
  return t.content
}

// ---------- 落库 HTML ⇄ 画布 HTML（完全交给插件自己的 toEditor/fromEditor） ----------
function toEditorHtml(html: string): string {
  if (!html) return ''
  const doc = new DOMParser().parseFromString(`<div id="root">${html}</div>`, 'text/html')
  const root = doc.getElementById('root')
  if (!root) return html
  for (const reg of Object.values(nodes.value)) {
    root.querySelectorAll(reg.tag).forEach((el) => {
      try {
        const out = reg.toEditor(attrMap(el), (el.textContent || '').trim())
        el.replaceWith(...Array.from(fragment(out).childNodes))
      } catch (e) {
        console.error('[editor] toEditor failed', reg.tag, e)
      }
    })
  }
  return root.innerHTML
}

function fromEditorHtml(html: string): string {
  if (!html) return ''
  const doc = new DOMParser().parseFromString(`<div id="root">${html}</div>`, 'text/html')
  const root = doc.getElementById('root')
  if (!root) return html
  root.querySelectorAll('[data-fanke-node]').forEach((el) => {
    const tag = el.getAttribute('data-fanke-node') || ''
    const reg = nodes.value[tag]
    if (!reg) return
    try {
      const { attrs, caption } = reg.fromEditor(el as HTMLElement)
      const node = doc.createElement(tag)
      for (const [k, v] of Object.entries(attrs || {})) {
        if (v !== undefined && v !== null) node.setAttribute(k, String(v))
      }
      node.textContent = caption || ''
      el.replaceWith(node)
    } catch (e) {
      console.error('[editor] fromEditor failed', tag, e)
    }
  })
  return root.innerHTML
}

const syncValue = (ed: any) => {
  value.value = fromEditorHtml(ed.getContent())
  lastEmitted = value.value
  emit('update:modelValue', value.value)
}

const uploadImage = async (blobInfo: any, progress: (percent: number) => void) => {
  const media = await uploadWithRetry(
    new File([blobInfo.blob()], blobInfo.filename(), { type: blobInfo.blob().type }),
    progress,
  )
  return media.url
}

const init = {
  license_key: 'gpl',
  base_url: '/tinymce',
  suffix: '.min',
  height: 560,
  min_height: 420,
  autoresize_bottom_margin: 32,
  resize: false,
  object_resizing: false,
  toolbar_sticky: true,
  toolbar_sticky_offset: 0,
  menubar: 'file edit view insert format table tools',
  language: 'zh_CN',
  plugins: 'autoresize code codesample lists link table wordcount',
  toolbar:
    'undo redo | blocks | bold italic underline strikethrough | forecolor backcolor | alignleft aligncenter alignright | bullist numlist blockquote | link fankeInsert table | codesample code | removeformat',
  branding: false,
  promotion: false,
  convert_urls: false,
  relative_urls: false,
  verify_html: false,
  custom_elements: '~fanke-img',
  extended_valid_elements:
    'figure[class|style|data-fanke-node|des|des_pos|des_sty|src],figcaption[class|style],' +
    'img[src|alt|title|width|height|class|style|loading|data-fanke-node|des|des_pos|des_sty],span[class]',
  automatic_uploads: true,
  images_upload_handler: uploadImage,
  skin_url: '/tinymce/skins/ui/oxide',
  content_style: [
    'body { font-family: -apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;',
    '  font-size: 16px; line-height: 1.85; color: #1a1a1a; padding: 8px 12px 40px; }',
    'img { max-width: 100%; height: auto; border-radius: 8px; }',
    'h1 { font-size: 1.9em; line-height: 1.3; } h2 { font-size: 1.5em; margin: 1.6em 0 .6em; }',
    'h3 { font-size: 1.25em; margin: 1.4em 0 .5em; }',
    'p { margin: .9em 0; }',
    'blockquote { margin: 1em 0; padding: .4em 1em; border-left: 3px solid #dcdfe6; color: #606266; background: #fafafa; }',
    'pre { padding: 12px 14px; background: #f6f8fa; border-radius: 8px; overflow: auto; }',
    'code { background: #f2f3f5; padding: 1px 5px; border-radius: 4px; font-size: .92em; }',
    'table { border-collapse: collapse; width: 100%; } td,th { border: 1px solid #dcdfe6; padding: 6px 8px; }',
    '[data-fanke-node]:hover { outline: 1px dashed #c0c4cc; outline-offset: 2px; }',
    '[data-mce-selected], figure[data-mce-selected], img[data-mce-selected] {',
    '  background: transparent !important; outline: none !important; box-shadow: none !important; }',
  ].join('\n'),
  setup(ed: any) {
    editor.value = ed
    // 插入菜单：所有已注册节点 + 内置普通图片（插件没装就只剩下普通图片）
    ed.ui.registry.addMenuButton('fankeInsert', {
      icon: 'image',
      tooltip: '插入图片 / 插件节点',
      fetch: (callback: any) => {
        const items: any[] = Object.values(nodes.value).map((r) => ({
          type: 'menuitem',
          text: r.insertLabel || r.label,
          onAction: () => emit('insert-node', r.tag),
        }))
        items.push({ type: 'separator' })
        items.push({ type: 'menuitem', text: '普通 <img>', onAction: () => emit('media', 'plain') })
        callback(items)
      },
    })
    ed.on('change input undo redo', () => {
      userEdited = true
      syncValue(ed)
      ed.dispatch('ResizeContent')
    })
    ed.on('SelectionChange', () => {
      emit('selection-change', (ed.selection.getContent({ format: 'text' }) || '').trim())
    })
    // 双击插件节点 → 用插件自己的 schema 弹面板
    ed.on('dblclick', (e: any) => {
      try {
        e?.preventDefault?.()
        const el = (e.target as HTMLElement)?.closest?.('[data-fanke-node]') as HTMLElement | null
        if (!el) return
        const tag = el.getAttribute('data-fanke-node') || ''
        const reg = nodes.value[tag]
        if (!reg) return
        pendingNodeEl = el
        pendingTag = tag
        const payload = {
          tag,
          label: reg.label,
          sections: reg.panel?.sections || [],
          values: reg.read(el),
        }
        setTimeout(() => emit('edit-node', payload), 0)
      } catch (err) {
        console.error('[editor] 打开节点设置失败', err)
      }
    })
    ed.on('init', () => {
      const onLoad = () => ed.dispatch('ResizeContent')
      const body = ed.getBody()
      body.addEventListener('load', onLoad, true)
      ed.on('remove', () => body.removeEventListener('load', onLoad, true))
    })
  },
}

/** 插入普通 <img>（不经过任何插件） */
const insertPlainImage = (m: MediaItem) => {
  const ed = editor.value
  if (!ed) return
  ed.insertContent(`<img src="${esc(m.url)}" alt="${esc(m.filename)}">`)
  syncValue(ed)
}

/** 插入插件节点：由父组件在媒体库选中后调用 */
const insertNode = (tag: string, media: MediaItem) => {
  const ed = editor.value
  const reg = nodes.value[tag]
  if (!ed || !reg) return
  const attrs = reg.attrsFromMedia
    ? reg.attrsFromMedia({ url: media.url, filename: media.filename, mime: media.mime })
    : { src: media.url }
  const merged = { ...(reg.defaultAttrs?.() || {}), ...attrs }
  ed.insertContent(`${reg.toEditor(merged, '')}<p>&nbsp;</p>`)
  syncValue(ed)
}

/** 属性面板保存：交给插件 apply 写回画布节点 */
const applyNodeSettings = (values: Record<string, any>) => {
  const ed = editor.value
  const reg = nodes.value[pendingTag]
  if (!ed || !reg || !pendingNodeEl) return
  try {
    reg.apply(pendingNodeEl, values)
  } catch (e) {
    console.error('[editor] apply failed', pendingTag, e)
  }
  userEdited = true
  syncValue(ed)
}

const getContent = () => (editor.value ? fromEditorHtml(editor.value.getContent()) : value.value)
const uploadImages = async () => {
  if (editor.value) await editor.value.uploadImages()
  if (editor.value) syncValue(editor.value)
}

const getSelectionText = () => {
  const ed = editor.value
  return ed ? (ed.selection.getContent({ format: 'text' }) || '').trim() : ''
}
const escapeHtml = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
const insertSelectionText = (text: string) => {
  const ed = editor.value
  if (!ed || !text) return
  const html = escapeHtml(text).replace(/\n{2,}/g, '<br><br>').replace(/\n/g, '<br>')
  ed.insertContent(html)
  syncValue(ed)
}
const undo = () => {
  const ed = editor.value
  if (!ed) return
  ed.undoManager.undo()
  syncValue(ed)
}

defineExpose({ insertNode, insertPlainImage, applyNodeSettings, getContent, uploadImages, getSelectionText, insertSelectionText, undo })

watch(
  () => props.modelValue,
  (next) => {
    value.value = next || ''
    if (!editor.value || value.value === lastEmitted) return
    if (!value.value.trim() && fromEditorHtml(editor.value.getContent()).trim()) return
    if (fromEditorHtml(editor.value.getContent()) !== value.value) {
      editor.value.setContent(toEditorHtml(value.value))
      lastEmitted = value.value
    }
  },
)

// 注册表变化（插件脚本刚加载完）时，若用户还没编辑，按最新能力重排一次
watch(
  nodes,
  () => {
    if (!editor.value || userEdited) return
    const current = fromEditorHtml(editor.value.getContent())
    if (current.trim()) editor.value.setContent(toEditorHtml(current))
  },
  { deep: false },
)

onMounted(async () => {
  void ensureContributions()
  if (!import.meta.client) return
  await ensureLoaded() // 先拿到插件注册表，再决定初始 HTML 怎么转换
  value.value = toEditorHtml(props.modelValue || '')
  await import('tinymce/tinymce')
  await Promise.all([
    import('tinymce/icons/default'),
    import('tinymce/themes/silver'),
    import('tinymce/models/dom'),
    import('tinymce/plugins/autoresize'),
    import('tinymce/plugins/code'),
    import('tinymce/plugins/codesample'),
    import('tinymce/plugins/link'),
    import('tinymce/plugins/lists'),
    import('tinymce/plugins/table'),
    import('tinymce/plugins/wordcount'),
  ])
  await import('tinymce-i18n/langs7/zh_CN.js')
  if (!disposed) ready.value = true
})
</script>

<template>
  <ClientOnly>
    <div class="rich-editor">
      <Editor
        v-if="ready"
        v-model="value"
        :init="init"
        @init="(_e: any, ed: any) => { editor = ed }"
        license-key="gpl"
      />
      <div v-else class="rich-editor-loading">编辑器加载中…</div>
    </div>
    <template #fallback><div class="rich-editor-loading">编辑器加载中…</div></template>
  </ClientOnly>
</template>

<style scoped>
.rich-editor-loading {
  padding: 40px 0;
  color: var(--muted, #909399);
}
</style>
