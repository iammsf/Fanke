<script setup lang="ts">
// 插件(扩展)管理:上传 .zip/.7z 安装、列表展示、启用/禁用/卸载/检查更新。
// 样式先简单,后续再统一美化。
definePageMeta({ layout: 'admin', middleware: 'auth' })
import type { Extension, ExtensionUpdateInfo } from '~/types'
import { ElMessageBox } from 'element-plus'

const { api, getToken, tryRefresh, apiBase } = useAuth()
const { push } = useToast()
const { run } = useBusy()
const { refreshPublic, refreshContributions } = useExtensions()

const loading = ref(true)
const error = ref<any>(null)
const items = ref<Extension[]>([])
const hooks = ref<string[]>([])

// 上传
const fileEl = ref<HTMLInputElement>()
const uploading = ref(false)
const uploadError = ref('')
const missingHooks = ref<string[]>([])

// 每个插件的操作状态
const busy = ref<Record<string, boolean>>({})
const updates = ref<Record<string, ExtensionUpdateInfo | 'error'>>({})

const load = async () => {
  loading.value = true
  error.value = null
  selected.value.clear() // 列表重载后旧 slug 可能已不存在,清空选中
  try {
    const res = await api<{ items: Extension[]; hooks: string[] }>(`/admin/extensions`)
    items.value = res.items || []
    hooks.value = res.hooks || []
  } catch (e: any) {
    error.value = e
  } finally {
    loading.value = false
  }
}
onMounted(load)

// XHR 上传(插件包可能较大,fetch 无进度)
function xhrUpload(file: File): Promise<{ id: number; slug: string }> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    const fd = new FormData()
    fd.append('file', file)
    xhr.open('POST', `${apiBase}/admin/extensions`)
    const t = getToken()
    if (t) xhr.setRequestHeader('Authorization', `Bearer ${t}`)
    xhr.withCredentials = true
    xhr.onload = () => {
      let data: any = null
      try {
        data = JSON.parse(xhr.responseText)
      } catch {
        /* ignore */
      }
      if (xhr.status >= 200 && xhr.status < 300) resolve(data)
      else {
        const err: any = new Error(data?.error || `上传失败(${xhr.status})`)
        err.status = xhr.status
        err.missing = data?.missing_hooks
        reject(err)
      }
    }
    xhr.onerror = () => reject(new Error('网络错误'))
    xhr.send(fd)
  })
}

const pick = () => fileEl.value?.click()

const upload = async () => {
  const file = fileEl.value?.files?.[0]
  if (!file) return
  const name = file.name.toLowerCase()
  if (!name.endsWith('.zip') && !name.endsWith('.7z')) {
    push('仅支持 .zip 或 .7z 压缩包', false)
    return
  }
  uploading.value = true
  uploadError.value = ''
  missingHooks.value = []
  try {
    let res: { id: number; slug: string }
    res = await run('上传中…', async () => {
      try {
        return await xhrUpload(file)
      } catch (e: any) {
        if (e.status === 401 && (await tryRefresh())) return await xhrUpload(file)
        throw e
      }
    })
    push(`已安装:${res.slug}`, true)
    if (fileEl.value) fileEl.value.value = ''
    await load()
    // 跳到详情页完成初始化
    navigateTo(`/admin/plugins/${res.slug}`)
  } catch (e: any) {
    uploadError.value = e.message || '上传失败'
    if (Array.isArray(e.missing)) missingHooks.value = e.missing
    push(e.message || '上传失败', false)
  } finally {
    uploading.value = false
  }
}

const toggle = async (e: Extension) => {
  const target = !e.enabled
  if (target && e.slug === 's3-storage') {
    try {
      await ElMessageBox.confirm('启用「S3 对象存储」？它会以博客服务账号运行，请仅启用你信任的插件。', '启用可信程序', {
        type: 'warning',
        confirmButtonText: '启用',
        cancelButtonText: '取消',
      })
    } catch {
      return
    }
  }
  busy.value[e.slug] = true
  try {
    await run(target ? '启用中…' : '停用中…', () => api(`/admin/extensions/${e.slug}/enabled`, { method: 'POST', body: JSON.stringify({ enabled: target }) }))
    e.enabled = target
    push(target ? '已启用' : '已禁用', true)
    await refreshPublic()
    await refreshContributions()
  } catch (err: any) {
    push(err.message || '操作失败', false)
  } finally {
    busy.value[e.slug] = false
  }
}

const remove = async (e: Extension) => {
  try {
    await ElMessageBox.confirm(
      `卸载「${e.name}」？插件文件与 extensions_data 记录将被删除；插件自己创建的数据表（如 image_focus_data）保留，需要时可手动清理。`,
      '卸载插件',
      { type: 'warning', confirmButtonText: '卸载', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  busy.value[e.slug] = true
  try {
    await run('卸载中…', () => api(`/admin/extensions/${e.slug}`, { method: 'DELETE' }))
    push('已卸载', true)
    await load()
    await refreshPublic()
    await refreshContributions()
  } catch (err: any) {
    push(err.message || '卸载失败', false)
  } finally {
    busy.value[e.slug] = false
  }
}

const checkUpdate = async (e: Extension) => {
  busy.value[`u:${e.slug}`] = true
  try {
    const r = await run('检查更新中…', () => api<ExtensionUpdateInfo>(`/admin/extensions/${e.slug}/check-update`, { method: 'POST' }))
    updates.value[e.slug] = r
    push(r.has_update ? `有新版本 ${r.version}` : '已是最新', !r.has_update)
  } catch (err: any) {
    updates.value[e.slug] = 'error'
    push(err.message || '检查更新失败', false)
  } finally {
    busy.value[`u:${e.slug}`] = false
  }
}

const updateExt = async (e: Extension) => {
  const u = updates.value[e.slug]
  if (!u || u === 'error' || !u.has_update) return
  try {
    await ElMessageBox.confirm(
      `把「${e.name}」更新到 ${u.version}？插件数据表会保留，已启用状态不变。`,
      '更新插件',
      { type: 'warning', confirmButtonText: '更新', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  busy.value[`u:${e.slug}`] = true
  try {
    await run('更新插件中…', () => api(`/admin/extensions/${e.slug}/update`, { method: 'POST' }))
    push('插件已更新', true)
    updates.value[e.slug] = 'error'
    await load()
    refreshPublic()
    refreshContributions()
  } catch (err: any) {
    push(err.message || '更新失败', false)
  } finally {
    busy.value[`u:${e.slug}`] = false
  }
}

// AppBadge → el-tag 类型映射
const badgeType = (k: string) => (({ pub: 'success', draft: 'primary', spam: 'danger', wait: 'warning', st: 'info' }) as Record<string, string>)[k] || 'info'
// 已检查过的更新结果(排除失败状态)
const upOf = (slug: string) => {
  const u = updates.value[slug]
  return u && u !== 'error' ? u : null
}
const upVersion = (slug: string) => upOf(slug)?.version || ''

// ---------- 批量操作(范式同 MediaLibrary,但键是 slug 字符串数组) ----------
const selected = ref<Set<string>>(new Set())
const toggleSel = (slug: string) => {
  if (selected.value.has(slug)) selected.value.delete(slug)
  else selected.value.add(slug)
}
const allChecked = computed(() => items.value.length > 0 && items.value.every((i) => selected.value.has(i.slug)))
const toggleSelAll = () => {
  const all = items.value.every((i) => selected.value.has(i.slug))
  items.value.forEach((i) => {
    if (all) selected.value.delete(i.slug)
    else selected.value.add(i.slug)
  })
}

const bulkEnabled = async (enabled: boolean) => {
  if (!selected.value.size) return
  const slugs = Array.from(selected.value)
  // 与单个启用一致:含 s3-storage 时先确认可信程序
  if (enabled && slugs.includes('s3-storage')) {
    try {
      await ElMessageBox.confirm('选中项包含「S3 对象存储」？它会以博客服务账号运行，请仅启用你信任的插件。', '启用可信程序', {
        type: 'warning',
        confirmButtonText: '启用',
        cancelButtonText: '取消',
      })
    } catch {
      return
    }
  }
  try {
    const res = await run(enabled ? '批量启用中…' : '批量停用中…', () =>
      api('/admin/extensions/bulk', { method: 'POST', body: JSON.stringify({ action: 'enabled', slugs, enabled }) }),
    )
    push(`已${enabled ? '启用' : '停用'} ${res.ok} 项`, true)
    if (res.failed?.length) (res.failed as string[]).forEach((f) => push(f, false))
    selected.value.clear()
    await load()
    await refreshPublic()
    await refreshContributions()
  } catch (e: any) {
    push(e.message || `批量${enabled ? '启用' : '停用'}失败`, false)
  }
}

const bulkUninstall = async () => {
  if (!selected.value.size) return
  const slugs = Array.from(selected.value)
  try {
    await ElMessageBox.confirm(
      `卸载选中的 ${slugs.length} 个插件？插件目录与 extensions_data 记录将被删除；插件自己创建的数据表保留。卸载后正文里的插件标签会按安装时登记的回落模板继续渲染，不会丢内容。`,
      '批量卸载',
      { type: 'warning', confirmButtonText: '卸载', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  try {
    const res = await run('批量卸载中…', () => api('/admin/extensions/bulk', { method: 'POST', body: JSON.stringify({ action: 'uninstall', slugs }) }))
    push(`已卸载 ${res.ok} 项`, true)
    if (res.failed?.length) (res.failed as string[]).forEach((f) => push(f, false))
    selected.value.clear()
    await load()
    await refreshPublic()
    await refreshContributions()
  } catch (e: any) {
    push(e.message || '批量卸载失败', false)
  }
}

useHead({ title: '插件 · fanke 后台' })
</script>

<template>
  <div>
    <section class="panel card">
      <div class="toolbar">
        <h2 style="margin: 0">插件</h2>
        <input ref="fileEl" type="file" accept=".zip,.7z" style="display: none" @change="upload" />
        <el-button type="primary" :disabled="uploading" @click="pick">
          {{ uploading ? '上传中…' : '上传插件包(.zip / .7z)' }}
        </el-button>
      </div>
      <p class="muted" style="margin-top: -4px">
        插件包须包含 info.json、init.js/init.ts、index.html;声明的 hook 若当前后端不支持,安装将被拒绝。
      </p>
      <p v-if="uploadError" style="color: var(--danger, #c0392b); margin: 6px 0 0">{{ uploadError }}</p>
      <p v-if="missingHooks.length" class="muted" style="margin: 4px 0 0">缺失 hook:{{ missingHooks.join(', ') }}</p>
    </section>

    <el-skeleton v-if="loading" :rows="8" animated />
    <ErrorBox v-else-if="error" :error="error" retry @retry="load" />

    <section v-else class="panel card">
      <p v-if="!items.length" class="muted">尚未安装任何插件。</p>
      <template v-else>
        <!-- 批量工具栏 -->
        <div class="media-batch">
          <el-checkbox class="batch-label" :model-value="allChecked" @change="toggleSelAll">全选</el-checkbox>
          <span v-if="selected.size" class="muted">已选 {{ selected.size }} 项</span>
          <template v-if="selected.size">
            <el-button size="small" @click="bulkEnabled(true)">启用</el-button>
            <el-button size="small" @click="bulkEnabled(false)">停用</el-button>
            <el-button size="small" type="danger" @click="bulkUninstall">卸载</el-button>
            <el-button size="small" text @click="selected.clear()">取消</el-button>
          </template>
        </div>
        <table class="ext-table">
          <thead>
            <tr>
              <th style="width: 36px"></th>
              <th style="width: 36px"></th>
              <th>插件</th>
              <th style="width: 160px">版本</th>
              <th style="width: 100px">状态</th>
              <th style="width: 340px">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="e in items" :key="e.slug">
              <td>
                <el-checkbox :model-value="selected.has(e.slug)" @change="toggleSel(e.slug)" />
              </td>
              <td>
              <img
                v-if="e.favico"
                :src="`${apiBase}/ext/${e.slug}/assets/${e.favico}`"
                alt=""
                style="width: 24px; height: 24px; border-radius: 6px; object-fit: cover"
              />
            </td>
            <td>
              <NuxtLink :to="`/admin/plugins/${e.slug}`" class="ext-name">{{ e.name }}</NuxtLink>
              <div class="muted" style="font-size: 12.5px">{{ e.description || '—' }}</div>
            </td>
            <td class="muted">
              <div class="ext-ver">
                <span>v{{ e.version }}</span>
                <el-tag
                  v-if="upOf(e.slug)?.has_update"
                  size="small"
                  type="primary"
                  effect="light"
                  class="ver-tag"
                >
                  → {{ upVersion(e.slug) }}
                </el-tag>
                <el-tag v-else-if="upOf(e.slug)" size="small" type="info" effect="plain" class="ver-tag">已最新</el-tag>
              </div>
            </td>
            <td>
              <el-tag v-if="!e.initialized" :type="badgeType('wait')" size="small" effect="light">未初始化</el-tag>
              <el-tag v-else-if="e.enabled" :type="badgeType('pub')" size="small" effect="light">已启用</el-tag>
              <el-tag v-else :type="badgeType('draft')" size="small" effect="light">已禁用</el-tag>
            </td>
            <td>
              <div class="ext-actions">
                <el-button size="small" @click="navigateTo(`/admin/plugins/${e.slug}`)">详情</el-button>
                <el-button v-if="e.update_url" size="small" :disabled="busy[`u:${e.slug}`]" @click="checkUpdate(e)">
                  检查更新
                </el-button>
                <el-button
                  v-if="upOf(e.slug)?.has_update"
                  size="small"
                  type="primary"
                  :disabled="busy[`u:${e.slug}`]"
                  @click="updateExt(e)"
                >
                  更新
                </el-button>
                <el-button size="small" :disabled="busy[e.slug] || (!e.initialized && !e.enabled)" @click="toggle(e)">
                  {{ e.enabled ? '禁用' : '启用' }}
                </el-button>
                <el-button size="small" type="danger" plain :disabled="busy[e.slug]" @click="remove(e)">卸载</el-button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
      </template>
    </section>

    <section class="panel card">
      <h3 style="margin-top: 0">当前后端可用 hook({{ hooks.length }})</h3>
      <div class="hooks">
        <code v-for="h in hooks" :key="h" class="hook">{{ h }}</code>
      </div>
    </section>
  </div>
</template>

<style scoped>
.ext-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 14.5px;
}
.ext-table th,
.ext-table td {
  padding: 10px 8px;
  text-align: left;
  border-bottom: 1px solid var(--border);
  vertical-align: middle;
}
.ext-table th {
  font-weight: 600;
  color: var(--muted);
  font-size: 13px;
}
.ext-name {
  font-weight: 600;
  color: var(--text);
}
.ext-name:hover {
  color: var(--primary);
}
.ext-ver {
  display: flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
}
.ext-ver > span {
  line-height: 1;
}
.ver-tag {
  display: inline-flex;
  align-items: center;
  height: 20px;
  margin: 0;
  padding: 0 6px;
}
/* 标签里的文字也一样强制垂直居中,不依赖 EP 的 line-height */
.ver-tag :deep(.el-tag__content) {
  display: inline-flex;
  align-items: center;
  line-height: 1;
}
.ext-actions {
  display: flex;
  flex-wrap: nowrap;
  gap: 6px;
}
/* Element Plus 会给相邻按钮加 12px 外边距,和 flex gap 叠加后会把操作列挤到换行 */
.ext-actions :deep(.el-button + .el-button) {
  margin-left: 0;
}
.hooks {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.hook {
  padding: 2px 8px;
  border-radius: 6px;
  background: var(--primary-soft);
  color: var(--primary);
  font-size: 12.5px;
}
</style>
