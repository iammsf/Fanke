<script setup lang="ts">
// 评论审核:按状态筛选,操作 通过/垃圾/删除
definePageMeta({ layout: 'admin', middleware: ['auth', 'admin'] })
import type { AdminComment, AdminPost, Page } from '~/types'

const { api } = useAuth()
const { push } = useToast()
const { run } = useBusy()

const PER = 30
const TABS = [
  { value: 'pending', label: '待审' },
  { value: 'approved', label: '已通过' },
  { value: 'spam', label: '垃圾' },
  { value: '', label: '全部' },
] as const

const status = ref('pending')
const page = ref(1)
const data = ref<Page<AdminComment> | null>(null)
const loading = ref(true)
const error = ref<any>(null)
const postTitles = ref<Record<number, string>>({})

const loadTitles = async () => {
  if (Object.keys(postTitles.value).length) return
  try {
    const d = await api<Page<AdminPost>>('/admin/posts?per=200&page=1')
    for (const p of d.items) postTitles.value[p.id] = p.title
  } catch {
    /* 拿不到映射就只显示 id */
  }
}

let loadSeq = 0

const load = async () => {
  const my = ++loadSeq
  loading.value = true
  error.value = null
  await loadTitles()
  if (my !== loadSeq) return
  try {
    const res = await api<Page<AdminComment>>(
      `/admin/comments?status=${encodeURIComponent(status.value)}&per=${PER}&page=${page.value}`,
    )
    if (my !== loadSeq) return
    data.value = res
  } catch (e: any) {
    if (my !== loadSeq) return
    error.value = e
  } finally {
    if (my === loadSeq) loading.value = false
  }
}
onMounted(load)

const setStatus = async (id: number, s: string) => {
  try {
    await run('更新中…', () => api(`/admin/comments/${id}/status`, { method: 'PUT', body: JSON.stringify({ status: s }) }))
    push(s === 'approved' ? '已通过' : '已标记垃圾', true)
    load()
  } catch (e: any) {
    push(e.message || '操作失败', false)
  }
}

const del = async (c: AdminComment) => {
  try {
    await ElMessageBox.confirm(`删除 ${c.author_name} 的这条评论?`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await run('删除中…', () => api(`/admin/comments/${c.id}`, { method: 'DELETE' }))
    push('已删除', true)
    load()
  } catch (e: any) {
    push(e.message || '删除失败', false)
  }
}

const badgeKind = (s: string) => (s === 'approved' ? 'pub' : s === 'spam' ? 'spam' : 'wait') as const
const badgeText = (s: string) => (s === 'approved' ? '已通过' : s === 'spam' ? '垃圾' : '待审')
// AppBadge → el-tag 类型映射
const badgeType = (k: string) => (({ pub: 'success', draft: 'primary', spam: 'danger', wait: 'warning', st: 'info' }) as Record<string, string>)[k] || 'info'

// ---------- 批量操作(范式同 MediaLibrary:选中 Set + 全选本页 + run/api/push) ----------
const selected = ref<Set<number>>(new Set())
const toggle = (id: number) => {
  if (selected.value.has(id)) selected.value.delete(id)
  else selected.value.add(id)
}
const allChecked = computed(() => (data.value?.items.length ?? 0) > 0 && data.value!.items.every((i) => selected.value.has(i.id)))
const toggleAll = () => {
  const ids = data.value?.items.map((i) => i.id) ?? []
  const all = ids.every((id) => selected.value.has(id))
  ids.forEach((id) => {
    if (all) selected.value.delete(id)
    else selected.value.add(id)
  })
}

const bulk = async (action: 'status' | 'delete', s?: 'approved' | 'pending' | 'spam') => {
  if (!selected.value.size) return
  const ids = Array.from(selected.value)
  const label = action === 'delete' ? '删除' : s === 'approved' ? '通过' : s === 'pending' ? '转待审' : '标记垃圾'
  if (action === 'delete') {
    try {
      await ElMessageBox.confirm(`删除选中的 ${ids.length} 条评论?此操作不可恢复。`, '批量删除', {
        type: 'warning',
        confirmButtonText: '删除',
        cancelButtonText: '取消',
      })
    } catch {
      return
    }
  }
  try {
    const body: Record<string, unknown> = { action, ids }
    if (action === 'status') body.status = s
    const res = await run(`批量${label}中…`, () => api('/admin/comments/bulk', { method: 'POST', body: JSON.stringify(body) }))
    push(`已${label} ${res.ok} 项`, true)
    if (res.failed?.length) (res.failed as string[]).forEach((f) => push(f, false))
    selected.value.clear()
    load()
  } catch (e: any) {
    push(e.message || `批量${label}失败`, false)
  }
}

useHead({ title: '评论 · fanke 后台' })
</script>

<template>
  <section class="panel card">
    <div class="toolbar">
      <h2 style="margin: 0">评论</h2>
      <div style="display: flex; gap: 8px; flex-wrap: wrap; margin-left: 12px">
        <el-button
          v-for="t in TABS"
          :key="t.value"
          size="small"
          :type="status === t.value ? 'primary' : 'default'"
          @click="((status = t.value), (page = 1), selected.clear(), load())"
          >{{ t.label }}</el-button
        >
      </div>
    </div>

    <el-skeleton v-if="loading" :rows="6" animated />
    <ErrorBox v-else-if="error" :error="error.status === 403 ? { message: '需要管理员角色才能查看评论' } : error" :retry="error.status !== 403" @retry="load" />
    <p v-else-if="!data?.items.length" class="muted">这个分类下暂时没有评论。</p>
    <template v-else>
      <!-- 批量工具栏 -->
      <div class="media-batch">
        <el-checkbox class="batch-label" :model-value="allChecked" @change="toggleAll">全选(本页)</el-checkbox>
        <span v-if="selected.size" class="muted">已选 {{ selected.size }} 项</span>
        <template v-if="selected.size">
          <el-button size="small" type="primary" @click="bulk('status', 'approved')">通过</el-button>
          <el-button size="small" @click="bulk('status', 'pending')">待审</el-button>
          <el-button size="small" @click="bulk('status', 'spam')">标垃圾</el-button>
          <el-button size="small" type="danger" @click="bulk('delete')">删除</el-button>
          <el-button size="small" text @click="selected.clear()">取消</el-button>
        </template>
      </div>
      <div v-for="c in data.items" :key="c.id" class="panel card">
        <div class="toolbar" style="margin-bottom: 6px">
          <el-checkbox :model-value="selected.has(c.id)" @change="toggle(c.id)" style="margin-right: 2px" />
          <strong>{{ c.author_name }}</strong>
          <el-tag :type="badgeType(badgeKind(c.status))" size="small" effect="light">{{ badgeText(c.status) }}</el-tag>
          <span class="muted">{{ fmtDateTime(c.created_at) }}</span>
          <NuxtLink v-if="postTitles[c.post_id]" class="muted" :to="`/admin/posts/${c.post_id}`" style="margin-left: auto">
            《{{ postTitles[c.post_id] }}》
          </NuxtLink>
          <span v-else class="muted" style="margin-left: auto">文章 #{{ c.post_id }}</span>
        </div>
        <div class="prose" style="font-size: 15px" v-html="c.content" />
        <div style="display: flex; gap: 8px; margin-top: 10px">
          <el-button v-if="c.status !== 'approved'" size="small" type="primary" @click="setStatus(c.id, 'approved')">通过</el-button>
          <el-button v-if="c.status !== 'spam'" size="small" @click="setStatus(c.id, 'spam')">标记垃圾</el-button>
          <el-button size="small" type="danger" @click="del(c)">删除</el-button>
        </div>
      </div>
      <el-pagination
        background
        layout="total, prev, pager, next, jumper"
        :page-size="PER"
        :total="data.total"
        :current-page="page"
        @current-change="(n) => { page = n; selected.clear(); load() }"
        style="margin-top:16px;justify-content:center;display:flex"
      />
    </template>
  </section>
</template>
