<script setup lang="ts">
// 插件详情页:上半区展示 info.json 字段(favicon 左、信息右),下半区 iframe 展示插件自带 index.html。
// iframe 与父页通过 postMessage 桥通信:插件内 Fanke.callHook → 父页代发鉴权请求 → 结果回传。
definePageMeta({ layout: 'admin', middleware: 'auth' })
import type { Extension, ExtensionUpdateInfo } from '~/types'

const route = useRoute()
const slug = computed(() => String(route.params.slug))
const { api, apiBase } = useAuth()
const { push } = useToast()
const { run } = useBusy()
const { refreshPublic, refreshContributions } = useExtensions()

const ext = ref<Extension | null>(null)
const loading = ref(true)
const error = ref<any>(null)
const busy = ref(false)
const update = ref<ExtensionUpdateInfo | null>(null)
const iframeEl = ref<HTMLIFrameElement>()
const iframeKey = ref(0) // 变更强制重载
// 插件 notify.send:全局通知/区域内提示/弹出框
const { inline: notices, show: showNotice, dismiss: dismissNotice } = usePluginNotify()
// 会写库的 hook:仅这些在桥上套全屏遮罩(读类 hook 在插件页加载时即触发,不能套)
const WRITE_HOOKS = new Set(['store.ensure_table', 'store.save', 'store.import', 'store.dedup', 'store.delete'])

const load = async () => {
  loading.value = true
  error.value = null
  try {
    ext.value = await api<Extension>(`/admin/extensions/${slug.value}`)
  } catch (e: any) {
    error.value = e
  } finally {
    loading.value = false
  }
}
onMounted(load)

const iframeSrc = computed(() => {
  if (!ext.value) return ''
  const init = ext.value.initialized ? '' : '?init=1'
  return `${apiBase}/ext/${ext.value.slug}/page${init}`
})

let resizeObserver: ResizeObserver | undefined
let mutationObserver: MutationObserver | undefined
let resizeFrame = 0
let removeFrameListeners: (() => void) | undefined
const disconnectFrame = () => {
  resizeObserver?.disconnect()
  mutationObserver?.disconnect()
  removeFrameListeners?.()
  removeFrameListeners = undefined
  cancelAnimationFrame(resizeFrame)
}
const onFrameLoad = () => {
  disconnectFrame()
  const frame = iframeEl.value
  const doc = frame?.contentDocument
  if (!frame || !doc?.body) return
  const sizingStyle = doc.createElement('style')
  // 内嵌页面只负责内容布局，滚动交给宿主页；避免 100vh/min-height 反过来撑高 iframe。
  sizingStyle.textContent = `html, body { height: auto !important; min-height: 0 !important; max-height: none !important; overflow: hidden !important; } body { display: flow-root !important; }`
  doc.head.append(sizingStyle)
  const updateHeight = () => {
    if (frame.contentDocument !== doc || !frame.isConnected) return
    const body = doc.body
    const view = doc.defaultView!
    const box = body.getBoundingClientRect()
    const style = view.getComputedStyle(body)
    const range = doc.createRange()
    range.selectNodeContents(body)
    const contentBottom = range.getBoundingClientRect().bottom + view.scrollY
    // 盒子高度包含 padding、border 和被 flow-root 收纳的末尾 margin；Range 补足溢出内容。
    const bottom = Math.max(box.bottom + view.scrollY, box.top + view.scrollY + body.scrollHeight, contentBottom)
    const height = Math.max(1, Math.ceil(bottom + (parseFloat(style.marginBottom) || 0)) + 2)
    if (frame.style.height !== `${height}px`) frame.style.height = `${height}px`
  }
  const measure = () => {
    cancelAnimationFrame(resizeFrame)
    if (document.hidden) updateHeight()
    else resizeFrame = requestAnimationFrame(updateHeight)
  }
  resizeObserver = new ResizeObserver(measure)
  resizeObserver.observe(doc.body)
  mutationObserver = new MutationObserver(measure)
  mutationObserver.observe(doc.body, { childList: true, subtree: true, attributes: true, characterData: true })
  doc.addEventListener('load', measure, true)
  window.addEventListener('resize', measure)
  removeFrameListeners = () => {
    doc.removeEventListener('load', measure, true)
    window.removeEventListener('resize', measure)
    sizingStyle.remove()
  }
  void doc.fonts.ready.then(measure)
  measure()
}
watch(iframeKey, disconnectFrame)
onBeforeUnmount(disconnectFrame)

const configRoutes: Record<string, Record<string, { path: string; method: string }>> = {
  's3-storage': {
    get: { path: '/admin/media/s3', method: 'GET' },
    put: { path: '/admin/media/s3', method: 'PUT' },
    buckets: { path: '/admin/media/s3/buckets', method: 'POST' },
    test: { path: '/admin/media/s3/test', method: 'POST' },
  },
  'image-processing': {
    get: { path: '/admin/media/image-processing', method: 'GET' },
    put: { path: '/admin/media/image-processing', method: 'PUT' },
  },
}

// ---------- postMessage 桥 ----------
const onMessage = async (ev: MessageEvent) => {
  // 同源校验 + 只处理来自本页 iframe 的消息
  if (ev.origin !== location.origin) return
  if (!iframeEl.value || ev.source !== iframeEl.value.contentWindow) return
  const d: any = ev.data
  if (!d || typeof d !== 'object') return

  if (d.type === 'fanke:plugin-config') {
    if (typeof d.id !== 'string' || typeof d.action !== 'string') return
    const source = ev.source as Window
    const currentSlug = slug.value
    const routes = Object.hasOwn(configRoutes, currentSlug) ? configRoutes[currentSlug] : undefined
    const endpoint = routes && Object.hasOwn(routes, d.action) ? routes[d.action] : undefined
    let result: unknown = null
    let error = ''
    try {
      if (!endpoint) throw new Error('不允许的插件配置操作')
      result = await api(endpoint.path, {
        method: endpoint.method,
        ...(endpoint.method === 'GET' ? {} : { body: JSON.stringify(d.payload ?? {}) }),
      })
      if (d.action === 'put') await refreshContributions()
    } catch (e: any) {
      error = e.message || '配置操作失败'
    }
    if (iframeEl.value?.contentWindow === source && slug.value === currentSlug) {
      source.postMessage({ type: 'fanke:plugin-config:result', id: d.id, result, error }, ev.origin)
    }
    return
  }

  if (d.type === 'fanke:hook') {
    const { id, hook, payload } = d
    // 建表属于有副作用的写操作,必须经用户同意才执行,避免插件未经授权创建数据表。
    if (hook === 'store.ensure_table') {
      try {
        await ElMessageBox.confirm(
          `插件「${ext.value?.name || slug.value}」申请创建专属数据表(ext_data_${slug.value.replace(/-/g, '_')})。该表仅本插件可读写,用于存储其运行数据。是否允许?`,
          '插件数据权限申请',
          { type: 'warning', confirmButtonText: '允许并建表', cancelButtonText: '拒绝' },
        )
      } catch {
        iframeEl.value.contentWindow?.postMessage(
          { type: 'fanke:hook:result', id, ok: false, data: null, error: '用户拒绝建表' },
          location.origin,
        )
        return
      }
    }
    let ok = true
    let data: any = null
    let errorMsg = ''
    try {
      const call = () => api<{ ok: boolean; data: any }>(
        `/admin/extensions/${slug.value}/hook/${encodeURIComponent(hook)}`,
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
    iframeEl.value.contentWindow?.postMessage(
      { type: 'fanke:hook:result', id, ok, data, error: errorMsg },
      location.origin,
    )
    return
  }

  if (d.type === 'fanke:init-done') {
    try {
      await run('初始化中…', () => api(`/admin/extensions/${slug.value}/initialized`, { method: 'POST' }))
      push('初始化完成', true)
      await load()
      iframeKey.value++ // 用非 init 模式重载插件页
    } catch (e: any) {
      push(e.message || '标记初始化失败', false)
    }
    return
  }

  if (d.type === 'fanke:emit') {
    // 插件主动推送的事件(如 "focus-saved"),此处仅 toast 提示
    if (d.ev === 'toast' && d.detail?.message) push(String(d.detail.message), d.detail.ok !== false)
    return
  }
}
onMounted(() => window.addEventListener('message', onMessage))
onBeforeUnmount(() => window.removeEventListener('message', onMessage))

// ---------- 操作 ----------
const toggle = async () => {
  if (!ext.value) return
  if (!ext.value.enabled && (ext.value.slug === 's3-storage' || ext.value.slug === 'image-processing')) {
    try {
      await ElMessageBox.confirm('启用可信服务端程序？它会以博客服务账号运行，请仅启用你信任的插件。', '启用可信程序', {
        type: 'warning',
        confirmButtonText: '启用',
        cancelButtonText: '取消',
      })
    } catch {
      return
    }
  }
  busy.value = true
  try {
    await run(ext.value.enabled ? '停用中…' : '启用中…', () => api(`/admin/extensions/${slug.value}/enabled`, {
      method: 'POST',
      body: JSON.stringify({ enabled: !ext.value!.enabled }),
    }))
    ext.value.enabled = !ext.value.enabled
    push(ext.value.enabled ? '已启用' : '已禁用', true)
    await refreshPublic()
    await refreshContributions()
  } catch (e: any) {
    push(e.message || '操作失败', false)
  } finally {
    busy.value = false
  }
}

const remove = async () => {
  if (!ext.value) return
  try {
    await ElMessageBox.confirm(`卸载「${ext.value.name}」？`, '卸载插件', {
      type: 'warning',
      confirmButtonText: '卸载',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  busy.value = true
  try {
    await run('卸载中…', () => api(`/admin/extensions/${slug.value}`, { method: 'DELETE' }))
    push('已卸载', true)
    refreshPublic()
    navigateTo('/admin/plugins')
  } catch (e: any) {
    push(e.message || '卸载失败', false)
    busy.value = false
  }
}

const checkUpdate = async () => {
  if (!ext.value) return
  busy.value = true
  try {
    update.value = await run('检查更新中…', () => api<ExtensionUpdateInfo>(`/admin/extensions/${slug.value}/check-update`, { method: 'POST' }))
    push(update.value.has_update ? `有新版本 ${update.value.version}` : '已是最新', !update.value.has_update)
  } catch (e: any) {
    push(e.message || '检查更新失败', false)
  } finally {
    busy.value = false
  }
}

const reloadIframe = () => iframeKey.value++

// AppBadge → el-tag 类型映射
const badgeType = (k: string) => (({ pub: 'success', draft: 'primary', spam: 'danger', wait: 'warning', st: 'info' }) as Record<string, string>)[k] || 'info'

useHead(() => ({ title: `${ext.value?.name || '插件'} · fanke 后台` }))
</script>

<template>
  <div>
    <el-skeleton v-if="loading" :rows="8" animated />
    <ErrorBox v-else-if="error" :error="error" retry @retry="load" />
    <template v-else-if="ext">
      <!-- 上 layout: favicon 左 + info 右 -->
      <section class="panel card head">
        <div class="fav">
          <img
            v-if="ext.favico"
            :src="`${apiBase}/ext/${ext.slug}/assets/${ext.favico}`"
            :alt="ext.name"
          />
          <div v-else class="fav-fallback">❖</div>
        </div>
        <div class="meta">
          <div class="title-row">
            <h2 style="margin: 0">{{ ext.name }}</h2>
            <span class="muted">v{{ ext.version }}</span>
            <el-tag v-if="!ext.initialized" :type="badgeType('wait')" size="small" effect="light">未初始化</el-tag>
            <el-tag v-else-if="ext.enabled" :type="badgeType('pub')" size="small" effect="light">已启用</el-tag>
            <el-tag v-else :type="badgeType('draft')" size="small" effect="light">已禁用</el-tag>
          </div>
          <p v-if="ext.description" class="muted" style="margin: 4px 0 0">{{ ext.description }}</p>
          <div class="grid">
            <div><span class="k">作者</span>{{ ext.author }}</div>
            <div><span class="k">发布日期</span>{{ ext.date }}</div>
            <div>
              <span class="k">主页</span>
              <a v-if="ext.url" :href="ext.url" target="_blank" rel="noopener">{{ ext.url }}</a>
              <span v-else class="muted">—</span>
            </div>
            <div>
              <span class="k">update_url</span>
              <span v-if="ext.update_url" class="mono">{{ ext.update_url }}</span>
              <span v-else class="muted">未配置</span>
            </div>
            <div><span class="k">安装时间</span>{{ fmtDateTime(ext.installed_at) }}</div>
            <div><span class="k">最近变更</span>{{ fmtDateTime(ext.updated_at) }}</div>
          </div>
          <div class="hooks">
            <span class="k">声明的 hook</span>
            <code v-for="h in ext.hooks" :key="h" class="hook">{{ h }}</code>
            <span v-if="!ext.hooks.length" class="muted">无</span>
          </div>
          <div v-if="update" class="muted" style="font-size: 13px">
            <template v-if="update.has_update">远端版本 {{ update.version }}({{ update.date }})</template>
            <template v-else>已是最新版本</template>
          </div>
          <div style="display: flex; gap: 8px; flex-wrap: wrap; margin-top: 4px">
            <el-button v-if="ext.update_url" size="small" :disabled="busy" @click="checkUpdate">检查更新</el-button>
            <el-button size="small" :disabled="busy || (!ext.initialized && !ext.enabled)" @click="toggle">
              {{ ext.enabled ? '禁用' : '启用' }}
            </el-button>
            <el-button size="small" type="danger" :disabled="busy" @click="remove">卸载</el-button>
            <el-button size="small" @click="reloadIframe">重载页面</el-button>
          </div>
        </div>
      </section>

      <p v-if="!ext.initialized" class="muted" style="margin: 0 4px">
        首次安装:下方插件页正在执行 init.js 完成初始化(建表等);完成后此提示消失,然后才能启用插件。
      </p>

      <!-- 下 layout: iframe 展示插件 index.html -->
      <section class="panel card frame-wrap">
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
          v-if="iframeSrc"
          :key="iframeKey"
          ref="iframeEl"
          :src="iframeSrc"
          class="frame"
          sandbox="allow-scripts allow-same-origin allow-forms allow-downloads"
          title="插件页面"
          @load="onFrameLoad"
        />
      </section>
    </template>
  </div>
</template>

<style scoped>
.plugin-notices {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 10px;
}
.head {
  display: flex;
  gap: 18px;
  align-items: flex-start;
}
.fav img {
  width: 72px;
  height: 72px;
  border-radius: 14px;
  object-fit: cover;
  display: block;
}
.fav-fallback {
  width: 72px;
  height: 72px;
  border-radius: 14px;
  background: var(--primary-soft);
  color: var(--primary);
  font-size: 34px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.meta {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.title-row {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 4px 18px;
  font-size: 13.5px;
}
.k {
  color: var(--muted);
  margin-right: 8px;
  font-size: 12.5px;
}
.mono {
  font-family: ui-monospace, monospace;
  font-size: 12.5px;
  word-break: break-all;
}
.hooks {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.hook {
  padding: 1px 7px;
  border-radius: 6px;
  background: var(--primary-soft);
  color: var(--primary);
  font-size: 12px;
}
.frame-wrap {
  padding: 0;
  overflow: hidden;
}
.frame {
  display: block;
  width: 100%;
  height: 1px;
  border: 0;
  background: #fff;
  border-radius: inherit;
}
@media (max-width: 700px) {
  .head {
    flex-direction: column;
  }
}
</style>
