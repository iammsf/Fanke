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
const fb = useTagFallbacks()
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
  // 插件不在线(禁用/卸载/文件损坏)时,它声明过的标签先包成回落信封;
  // 已注册进编辑器的标签交给插件自己的 toEditor。
  html = fb.wrap(html, (tag) => !!nodes.value[tag])
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
  // 拆回落信封:原样还原落库标签,插件停用期间的编辑不会把它写坏
  root.querySelectorAll('[data-fanke-fallback]').forEach((el) => {
    const orig = el.getAttribute('data-fanke-orig') || ''
    if (!orig.trim()) {
      el.remove()
      return
    }
    el.replaceWith(...Array.from(fragment(orig).childNodes))
  })
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

// v-model 绑定给 TinyMCE 的必须是「画布 HTML」(figure 形态)；
// 落库 HTML(fanke-img 形态)只通过 emit 往外发,绝不回灌编辑器,
// 否则 tinymce-vue 的 watch 会 setContent 一次,把插件节点解析成裸标签并丢属性。
const syncValue = (ed: any) => {
  // 保存前若还停在内联 HTML 编辑态,先把它还原成真实标签
  if (editEl && !applyingEdit) applyInlineHtmlEdit(true)
  const canvas = ed.getContent()
  value.value = canvas
  const out = fromEditorHtml(canvas)
  lastEmitted = out
  emit('update:modelValue', out)
}

const uploadImage = async (blobInfo: any, progress: (percent: number) => void) => {
  const media = await uploadWithRetry(
    new File([blobInfo.blob()], blobInfo.filename(), { type: blobInfo.blob().type }),
    progress,
  )
  return media.url
}

// ---------- 右键「编辑 HTML」:整块(含 p/a/img 等外层标签)在画布内变成可编辑代码 ----------
const inlineEditing = ref(false)
// 选区覆盖到的正文顶层块(直接挂在 body 下的节点),含外层标签
let pendingHtml: { html: string; first: HTMLElement; last: HTMLElement } | null = null
let editEl: HTMLElement | null = null
let editOrig = ''
let applyingEdit = false

// 只清理正文首尾完全空白的段落,不碰带图片/表格/代码块的容器
const stripEmptyEdgeBlocks = (body: HTMLElement) => {
  const emptyP = (el: Element | null) =>
    !!el && el.tagName === 'P' && !(el.textContent || '').trim() && !el.querySelector('img,figure,table,pre')
  let guard = 0
  while (emptyP(body.firstElementChild) && guard++ < 10) body.firstElementChild!.remove()
  guard = 0
  while (emptyP(body.lastElementChild) && guard++ < 10) body.lastElementChild!.remove()
}

/** 找到包含该节点的正文顶层块(直接挂在 body 下的元素)。 */
const topBlockOf = (ed: any, node: Node | null): HTMLElement | null => {
  const body = ed.getBody()
  let el: HTMLElement | null = node ? (node.nodeType === 1 ? (node as HTMLElement) : node.parentElement) : null
  if (!el || el === body) return null
  while (el.parentElement && el.parentElement !== body) el = el.parentElement
  return el
}

/** 右键时记录目标:选区覆盖到的整块(含外层标签),或光标所在块。 */
const captureHtmlTarget = (ed: any, target: HTMLElement | null) => {
  const body = ed.getBody()
  const sel = ed.selection
  let first: HTMLElement | null = null
  let last: HTMLElement | null = null
  if (!sel.isCollapsed()) {
    const rng = sel.getRng()
    first = topBlockOf(ed, rng.startContainer)
    last = topBlockOf(ed, rng.endContainer) || first
  } else {
    const host = target?.closest?.('[data-fanke-node]') as HTMLElement | null
    const cur = target && target !== body && body.contains(target) ? target : (sel.getNode() as HTMLElement | null)
    first = topBlockOf(ed, host || cur)
    last = first
  }
  if (!first || !last) {
    pendingHtml = null
    return
  }
  // 保证 first 在 last 之前(反向选择)
  if (first.compareDocumentPosition(last) & Node.DOCUMENT_POSITION_PRECEDING) {
    const tmp = first
    first = last
    last = tmp
  }
  const parts: string[] = []
  let n: Node | null = first
  while (n) {
    if (n.nodeType === 1) parts.push(ed.serializer.serialize(n as HTMLElement, { format: 'html' }))
    if (n === last) break
    n = n.nextSibling
  }
  pendingHtml = { html: parts.join('\n'), first, last }
}

/** 用给定 HTML 替换当前代码块(空内容则整块删掉)。 */
const replaceEditBlock = (ed: any, html: string) => {
  const el = editEl
  if (!el) return
  const parent = el.parentNode
  if (!parent) return
  if (!html.trim()) {
    ed.dom.remove(el)
    return
  }
  const tmp = ed.getDoc().createElement('div')
  tmp.innerHTML = html
  while (tmp.firstChild) parent.insertBefore(tmp.firstChild, el)
  ed.dom.remove(el)
  stripEmptyEdgeBlocks(ed.getBody())
}

/** 把当前代码块还原成真实 HTML。 */
const applyInlineHtmlEdit = (silent = false) => {
  const ed = editor.value
  if (!ed || !editEl) return
  const html = (editEl.textContent || '').trim()
  applyingEdit = true
  try {
    replaceEditBlock(ed, toEditorHtml(html))
    inlineEditing.value = false
    editEl = null
    editOrig = ''
    if (!silent) {
      userEdited = true
      syncValue(ed)
    }
  } catch {
    // 解析失败就还原,不让半截代码毁掉正文
    try {
      replaceEditBlock(ed, editOrig)
    } catch {
      /* ignore */
    }
    editEl = null
    editOrig = ''
    inlineEditing.value = false
  } finally {
    applyingEdit = false
  }
}

/** 取消编辑:还原成右键前的 HTML(含外层标签)。 */
const cancelInlineHtmlEdit = () => {
  const ed = editor.value
  if (!ed || !editEl) return
  applyingEdit = true
  try {
    replaceEditBlock(ed, toEditorHtml(editOrig))
  } catch {
    /* ignore */
  } finally {
    editEl = null
    editOrig = ''
    inlineEditing.value = false
    applyingEdit = false
  }
}

/** 把目标节点/选区换成可编辑的 HTML 代码块。 */
const startInlineHtmlEdit = (ed: any) => {
  if (!pendingHtml) return
  applyInlineHtmlEdit(true)
  const p = pendingHtml
  pendingHtml = null
  const parent = p.first.parentNode
  if (!parent) return
  const el = ed.getDoc().createElement('pre')
  el.className = 'fanke-html-edit'
  el.setAttribute('contenteditable', 'true')
  // 代码块里只露出落库形态的标签(<img> / <fanke-img>说明</fanke-img>),
  // 画布的 figure 包装是内部表示,不外露给用户写;应用时再转回画布形态。
  const src = fromEditorHtml(p.html)
  el.textContent = src
  // 代码块插到被选中的整块前面,再把原块删掉
  parent.insertBefore(el, p.first)
  let n: Node | null = p.first
  while (n) {
    const next: Node | null = n.nextSibling
    const isLast = n === p.last
    ed.dom.remove(n)
    if (isLast) break
    n = next
  }
  editEl = el
  editOrig = src
  inlineEditing.value = true
  ed.selection.setCursorLocation(el, 0)
  el.focus?.()
}

// 「工具 → 源代码」:与内联编辑同一口径,只展示/接收落库形态的标签,
// 不用 TinyMCE 自带 code 插件(它直接暴露画布 figure 包装)。
const openSourceDialog = (ed: any) => {
  ed.windowManager.open({
    title: '源代码',
    size: 'large',
    body: { type: 'panel', items: [{ type: 'textarea', name: 'code', label: 'HTML' }] },
    buttons: [
      { type: 'cancel', text: '取消' },
      { type: 'submit', text: '应用', primary: true },
    ],
    initialData: { code: fromEditorHtml(ed.getContent()) },
    onSubmit: (api: any) => {
      const code = String(api.getData().code || '')
      api.close()
      ed.setContent(toEditorHtml(code))
      userEdited = true
      syncValue(ed)
    },
  })
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
  menubar: 'edit view insert format table tools',
  // 源代码走自绘对话框(只露落库形态的标签),不挂 TinyMCE code 插件;「查看」里不放第二处。
  menu: {
    view: { title: '查看', items: 'visualaid' },
    tools: { title: '工具', items: 'fankeSource wordcount' },
  },
  language: 'zh_CN',
  plugins: 'autoresize codesample lists link table wordcount',
  toolbar:
    'undo redo | blocks | bold italic underline strikethrough | forecolor backcolor | alignleft aligncenter alignright | bullist numlist blockquote | link fankeInsert table | codesample | removeformat',
  branding: false,
  promotion: false,
  convert_urls: false,
  relative_urls: false,
  verify_html: false,
  custom_elements: '~fanke-img',
  // 右键菜单:保留 TinyMCE 自带的链接/表格菜单,末尾追加「编辑 HTML」
  contextmenu: 'link table fankeHtmlEdit',
  extended_valid_elements:
    'figure[class|style|data-fanke-node|des|des_pos|des_sty|src],figcaption[class|style],' +
    'pre[class|contenteditable],' +
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
    'pre.fanke-html-edit { background: #1f2430; color: #e6e6e6; padding: 14px 16px; border-radius: 8px;',
    '  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 12.5px;',
    '  line-height: 1.7; white-space: pre-wrap; outline: 2px solid #7c5cff; outline-offset: 1px; }',
    'code { background: #f2f3f5; padding: 1px 5px; border-radius: 4px; font-size: .92em; }',
    'table { border-collapse: collapse; width: 100%; } td,th { border: 1px solid #dcdfe6; padding: 6px 8px; }',
    '[data-fanke-node]:hover { outline: 1px dashed #c0c4cc; outline-offset: 2px; }',
    // 插件不在线时的回落信封:整块锁定(改它写不回落库标签),用暖色边框区别于正常插件节点
    'div.fanke-fallback-box { padding: 4px 6px; margin: .9em 0; border: 1px dashed #e6a23c;',
    '  border-radius: 8px; background: #fdf6ec; }',
    'div.fanke-fallback-box figure { margin: 0; text-align: center; }',
    'div.fanke-fallback-box img { max-width: 100%; height: auto; }',
    'div.fanke-fallback-box figcaption { margin-top: 4px; font-size: 13px; color: #909399; }',
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
      // 内联编辑 HTML 代码块时不要回写内容:每一帧都解析会把正文拆碎
      if (editEl) {
        ed.dispatch('ResizeContent')
        return
      }
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
    // 右键:只记录目标(不改动 TinyMCE 自己的右键菜单)
    ed.on('contextmenu', (e: any) => {
      captureHtmlTarget(ed, (e?.target as HTMLElement) || null)
    })
    // 「编辑 HTML」以 TinyMCE 菜单项形式挂进右键菜单,避免和原生菜单叠在一起
    ed.ui.registry.addMenuItem('fankeHtmlEdit', {
      text: '编辑 HTML',
      icon: 'code',
      onAction: () => startInlineHtmlEdit(ed),
    })
    ed.ui.registry.addMenuItem('fankeSource', {
      text: '源代码',
      icon: 'sourcecode',
      onAction: () => openSourceDialog(ed),
    })
    ed.ui.registry.addContextMenu('fankeHtmlEdit', {
      update: () => (pendingHtml ? ['fankeHtmlEdit'] : []),
    })
    // 代码块内:Ctrl+Enter 应用,Esc 取消
    ed.on('keydown', (e: any) => {
      if (!editEl) return
      if (e.key === 'Escape') {
        e.preventDefault?.()
        cancelInlineHtmlEdit()
      } else if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault?.()
        applyInlineHtmlEdit()
      }
    })
    // 点到代码块外面就算编辑完成
    ed.on('click', (e: any) => {
      if (!editEl) return
      const t = e.target as HTMLElement | null
      if (!t || !editEl.contains(t)) applyInlineHtmlEdit()
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

const getContent = () => {
  if (editor.value && editEl && !applyingEdit) applyInlineHtmlEdit(true)
  return editor.value ? fromEditorHtml(editor.value.getContent()) : value.value
}
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
    const incoming = next || ''
    if (!editor.value) {
      value.value = incoming
      return
    }
    // 自己刚发出去的落库 HTML,不回灌
    if (incoming === lastEmitted) return
    const canvas = toEditorHtml(incoming)
    if (canvas === editor.value.getContent()) return
    value.value = canvas
    editor.value.setContent(canvas)
    lastEmitted = incoming
  },
)

// 注册表变化（插件脚本刚加载完）时，若用户还没编辑，按最新能力重排一次
watch(
  nodes,
  () => {
    if (!editor.value || userEdited) return
    const canvas = editor.value.getContent()
    const converted = toEditorHtml(fromEditorHtml(canvas))
    if (!converted.trim() || converted === canvas) return
    value.value = converted
    editor.value.setContent(converted)
  },
  { deep: false },
)

onMounted(async () => {
  void ensureContributions()
  if (!import.meta.client) return
  await ensureLoaded() // 先拿到插件注册表，再决定初始 HTML 怎么转换
  await fb.load() // 回落账本与在线列表同源(/ext),插件不在线的标签要包成信封
  value.value = toEditorHtml(props.modelValue || '')
  await import('tinymce/tinymce')
  await Promise.all([
    import('tinymce/icons/default'),
    import('tinymce/themes/silver'),
    import('tinymce/models/dom'),
    import('tinymce/plugins/autoresize'),
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
      <div v-if="inlineEditing" class="inline-hint">
        <span>正在编辑 HTML</span>
        <button type="button" class="hint-btn primary" @click="applyInlineHtmlEdit()">应用</button>
        <button type="button" class="hint-btn" @click="cancelInlineHtmlEdit()">取消</button>
        <span class="hint-tip">Ctrl+Enter 应用 · Esc 取消</span>
      </div>
    </div>
    <template #fallback><div class="rich-editor-loading">编辑器加载中…</div></template>
  </ClientOnly>
</template>

<style scoped>
.rich-editor {
  position: relative;
}
.rich-editor-loading {
  padding: 40px 0;
  color: var(--muted, #909399);
}
.inline-hint {
  position: absolute;
  right: 14px;
  bottom: 14px;
  z-index: 10;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  border-radius: 999px;
  background: rgba(31, 36, 48, 0.92);
  color: #fff;
  font-size: 12px;
}
.hint-tip {
  color: rgba(255, 255, 255, 0.6);
}
.hint-btn {
  padding: 2px 10px;
  border-radius: 999px;
  border: 1px solid rgba(255, 255, 255, 0.35);
  background: transparent;
  color: #fff;
  font-size: 12px;
  cursor: pointer;
}
.hint-btn.primary {
  border-color: transparent;
  background: #7c5cff;
}
.hint-btn:hover {
  opacity: 0.9;
}
</style>
