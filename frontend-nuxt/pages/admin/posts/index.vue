<script setup lang="ts">
// 文章列表:状态筛选 + 搜索 + 分页;操作:编辑 / 发布 / 删除 / 公开开关
definePageMeta({ layout: 'admin', middleware: 'auth' })
import type { AdminPost, Page } from '~/types'
import { Picture } from '@element-plus/icons-vue'

const { api } = useAuth()
const { push } = useToast()
const { run } = useBusy() // 与 DB/存储交互期间的全屏加载遮罩(兼防连点)
const route = useRoute()

const PER = 20
const state = reactive({
  status: String(route.query.status || ''),
  q: '',
  page: 1,
})
const qInput = ref('')
const data = ref<Page<AdminPost> | null>(null)
const loading = ref(true)
const error = ref<any>(null)
const busyIds = reactive(new Set<number>())

const load = async () => {
  loading.value = true
  error.value = null
  const u = new URLSearchParams({ page: String(state.page), per: String(PER) })
  if (state.status) u.set('status', state.status)
  if (state.q) u.set('q', state.q)
  try {
    data.value = await api<Page<AdminPost>>(`/admin/posts?${u}`)
  } catch (e: any) {
    error.value = e
  } finally {
    loading.value = false
  }
}
onMounted(load)

const search = () => {
  state.q = qInput.value.trim()
  state.page = 1
  load()
}

const publish = async (p: AdminPost) => {
  busyIds.add(p.id)
  try {
    await run('发布中…', () => api(`/admin/posts/${p.id}/publish`, { method: 'POST' }))
    push(`已发布《${p.title}》`, true)
    load()
  } catch (e: any) {
    push(e.message || '发布失败', false)
  } finally {
    busyIds.delete(p.id)
  }
}

const del = async (p: AdminPost) => {
  try {
    await ElMessageBox.confirm(`确定删除《${p.title}》?此操作不可恢复。`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  busyIds.add(p.id)
  try {
    await run('删除中…', () => api(`/admin/posts/${p.id}`, { method: 'DELETE' }))
    push('已删除', true)
    load()
  } catch (e: any) {
    push(e.message || '删除失败', false)
    busyIds.delete(p.id)
  }
}

// 公开性快捷开关(仅更新 is_public,不动其他字段)
const togglePublic = async (p: AdminPost, val: boolean) => {
  try {
    await run('更新中…', () => api(`/admin/posts/${p.id}/public`, { method: 'POST', body: JSON.stringify({ is_public: val }) }))
    push(val ? `《${p.title}》已公开` : `《${p.title}》已取消公开`, true)
    p.is_public = val
  } catch (e: any) {
    push(e.message || '操作失败', false)
  }
}

// ---------- 置顶 ----------
const stickyList = ref<AdminPost[]>([])
const stickyOpen = ref(false)
const stickyDirty = ref(false)
const stickySaving = ref(false)
const dragIdx = ref(-1)

const loadSticky = async () => {
  try {
    const res = await api<{ items: AdminPost[] }>('/admin/posts-sticky')
    stickyList.value = res.items || []
    stickyDirty.value = false
  } catch {
    /* 忽略,面板显示为空 */
  }
}
onMounted(loadSticky)

const toggleSticky = async (p: AdminPost, val: boolean) => {
  try {
    await run('更新中…', () => api(`/admin/posts/${p.id}/sticky`, { method: 'POST', body: JSON.stringify({ sticky: val }) }))
    p.sticky = val ? Math.max(0, ...stickyList.value.map((s) => s.sticky)) + 1 : 0
    push(val ? `《${p.title}》已置顶` : `《${p.title}》已取消置顶`, true)
    loadSticky()
  } catch (e: any) {
    push(e.message || '操作失败', false)
  }
}

// HTML5 拖动排序:dragover 时即时换位,松手仅标记脏,点保存落库
const onDragStart = (i: number, e: DragEvent) => {
  dragIdx.value = i
  e.dataTransfer?.setData('text/plain', String(i))
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
}
const onDragOver = (i: number, e: DragEvent) => {
  e.preventDefault()
  const from = dragIdx.value
  if (from < 0 || from === i) return
  const item = stickyList.value.splice(from, 1)[0]
  if (!item) return
  stickyList.value.splice(i, 0, item)
  dragIdx.value = i
  stickyDirty.value = true
}
const onDragEnd = () => {
  dragIdx.value = -1
}

const saveStickyOrder = async () => {
  stickySaving.value = true
  try {
    const ids = stickyList.value.map((s) => s.id)
    await run('保存中…', () => api('/admin/posts-sticky', { method: 'PUT', body: JSON.stringify({ ids }) }))
    push('置顶顺序已保存', true)
    stickyDirty.value = false
    load()
  } catch (e: any) {
    push(e.message || '保存失败', false)
  } finally {
    stickySaving.value = false
  }
}

// 封面缩略图 → Element Plus 图片查看器
const viewerOpen = ref(false)
const viewerUrls = ref<string[]>([])
const viewerIndex = ref(0)
const openViewer = (p: AdminPost) => {
  const urls = (data.value?.items ?? []).filter((i) => i.cover_url).map((i) => i.cover_url as string)
  if (!urls.length) return
  viewerUrls.value = urls
  viewerIndex.value = Math.max(0, urls.indexOf(p.cover_url as string))
  viewerOpen.value = true
}

// AppBadge → el-tag 类型映射
const badgeType = (k: string) => (({ pub: 'success', draft: 'primary', spam: 'danger', wait: 'warning', st: 'info' }) as Record<string, string>)[k] || 'info'

useHead({ title: '文章 · fanke 后台' })
</script>

<template>
  <section class="panel card">
    <div class="toolbar">
      <h2 style="margin: 0">文章</h2>
      <el-select
        v-model="state.status"
        style="width: auto"
        @change="((state.page = 1), load())"
      >
        <el-option value="" label="全部状态" />
        <el-option value="published" label="已发布" />
        <el-option value="draft" label="草稿" />
      </el-select>
      <el-input v-model="qInput" placeholder="搜索标题/摘要/正文…" style="width: 220px" @keydown.enter="search" />
      <el-button @click="search">搜索</el-button>
      <el-button type="primary" style="margin-left: auto" @click="navigateTo('/admin/posts/new')">+ 新文章</el-button>
    </div>

    <!-- 置顶排序面板 -->
    <div class="panel card" style="margin: 12px 0; padding: 12px 16px">
      <div style="display: flex; align-items: center; gap: 10px">
        <strong>置顶文章({{ stickyList.length }})</strong>
        <span class="muted" style="font-size: 12.5px">拖动调整顺序,首页始终排在最前</span>
        <div style="margin-left: auto; display: flex; gap: 8px">
          <el-button v-if="stickyOpen && stickyList.length" size="small" type="primary" :disabled="!stickyDirty || stickySaving" @click="saveStickyOrder">保存顺序</el-button>
          <el-button size="small" @click="stickyOpen = !stickyOpen">{{ stickyOpen ? '收起' : '展开排序' }}</el-button>
        </div>
      </div>
      <div v-if="stickyOpen" style="margin-top: 10px">
        <p v-if="!stickyList.length" class="muted" style="margin: 4px 0">暂无置顶文章。在下方列表勾选「置顶」即可添加。</p>
        <ul v-else style="list-style: none; margin: 0; padding: 0">
          <li
            v-for="(s, i) in stickyList"
            :key="s.id"
            draggable="true"
            @dragstart="onDragStart(i, $event)"
            @dragover="onDragOver(i, $event)"
            @dragend="onDragEnd"
            style="display: flex; align-items: center; gap: 10px; padding: 8px 10px; border: 1px solid var(--line, #333); border-radius: 8px; margin-bottom: 6px; cursor: grab; background: var(--card, transparent)"
            :style="dragIdx === i ? 'opacity: .5' : ''"
          >
            <span class="muted" style="cursor: grab; user-select: none">⠿</span>
            <span class="muted" style="min-width: 20px">{{ i + 1 }}.</span>
            <NuxtLink :to="`/admin/posts/${s.id}`" style="color: var(--text); font-weight: 600">{{ s.title }}</NuxtLink>
            <el-tag v-if="s.status !== 'published'" :type="badgeType('draft')" size="small" effect="light">草稿</el-tag>
            <el-tag v-if="!s.is_public" :type="badgeType('draft')" size="small" effect="light">未公开</el-tag>
            <el-button size="small" style="margin-left: auto" @click="toggleSticky(s, false)">取消置顶</el-button>
          </li>
        </ul>
      </div>
    </div>

    <el-skeleton v-if="loading" :rows="6" animated />
    <ErrorBox v-else-if="error" :error="error" retry @retry="load" />
    <p v-else-if="!data?.items.length" class="muted">没有符合条件的文章。</p>
    <template v-else>
      <el-table :data="data.items" stripe row-key="id" class="posts-table">
        <el-table-column label="文章" min-width="300">
          <template #default="{ row: p }">
            <div class="post-cell">
              <img
                v-if="p.cover_url"
                :src="p.cover_url"
                :alt="p.title"
                title="点击预览"
                class="post-thumb"
                @click="openViewer(p)"
              />
              <span v-else class="post-thumb post-thumb-empty"><el-icon><Picture /></el-icon></span>
              <div class="post-main">
                <NuxtLink class="post-title" :to="`/admin/posts/${p.id}`">{{ p.title }}</NuxtLink>
                <div class="post-slug muted">{{ p.slug }}</div>
                <div class="post-tags">
                  <el-tag :type="badgeType(p.status === 'published' ? 'pub' : 'draft')" size="small" effect="light">
                    {{ p.status === 'published' ? '已发布' : '草稿' }}
                  </el-tag>
                  <el-tag v-if="p.sticky > 0" :type="badgeType('pub')" size="small" effect="light">置顶</el-tag>
                  <el-tag v-if="p.type === 'moment'" :type="badgeType('draft')" size="small" effect="light">随记</el-tag>
                  <el-tag v-if="p.status === 'published' && !p.is_public" :type="badgeType('draft')" size="small" effect="light">未公开</el-tag>
                </div>
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="日期" width="110">
          <template #default="{ row: p }"><span class="muted">{{ fmtDate(p.published_at || p.created_at) }}</span></template>
        </el-table-column>
        <el-table-column prop="views" label="阅读" width="76" align="right" />
        <el-table-column width="82" align="center">
          <template #header>
            <span title="公开:首页可刷到;取消则首页不可见,直接输 URL 也 404,仅站内文章跳转可打开">公开</span>
          </template>
          <template #default="{ row: p }">
            <el-switch :model-value="p.is_public !== false" @update:model-value="togglePublic(p, $event)" />
          </template>
        </el-table-column>
        <el-table-column width="82" align="center">
          <template #header>
            <span title="置顶(首页排在最前,可在上方拖动排序)">置顶</span>
          </template>
          <template #default="{ row: p }">
            <el-switch :model-value="p.sticky > 0" @update:model-value="toggleSticky(p, $event)" />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="168" fixed="right" align="right">
          <template #default="{ row: p }">
            <div class="ops">
              <el-button link type="primary" @click="navigateTo(`/admin/posts/${p.id}`)">编辑</el-button>
              <el-button v-if="p.status !== 'published'" link type="primary" :disabled="busyIds.has(p.id)" @click="publish(p)">发布</el-button>
              <el-button v-else link @click="navigateTo(`/posts/${encodeURIComponent(p.slug)}`)">查看</el-button>
              <el-button link type="danger" :disabled="busyIds.has(p.id)" @click="del(p)">删除</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        background
        layout="total, prev, pager, next, jumper"
        :page-size="PER"
        :total="data.total"
        :current-page="state.page"
        @current-change="(n) => { state.page = n; load() }"
        style="margin-top:16px;justify-content:center;display:flex"
      />
    </template>
  </section>

  <!-- Element Plus 图片查看器(本页封面) -->
  <el-image-viewer
    v-if="viewerOpen"
    :url-list="viewerUrls"
    :initial-index="viewerIndex"
    teleported
    hide-on-click-modal
    @close="viewerOpen = false"
  />
</template>

<style scoped>
.posts-table :deep(.el-table__cell) {
  padding: 9px 0;
}
.post-cell {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}
.post-thumb {
  flex: none;
  width: 64px;
  height: 40px;
  border-radius: 6px;
  object-fit: cover;
  display: block;
  background: var(--surface-2);
  cursor: zoom-in;
}
.post-thumb-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--muted);
  cursor: default;
}
.post-main {
  min-width: 0;
}
.post-title {
  display: block;
  color: var(--text);
  font-size: 14.5px;
  font-weight: 600;
  line-height: 1.45;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.post-title:hover {
  color: var(--primary);
  text-decoration: none;
}
.post-slug {
  font-size: 12px;
  line-height: 1.4;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.post-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 4px;
}
.ops {
  display: inline-flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
}
.ops :deep(.el-button + .el-button) {
  margin-left: 0;
}
</style>
