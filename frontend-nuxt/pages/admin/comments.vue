<script setup lang="ts">
// 评论审核:按状态筛选,操作 通过/垃圾/删除
definePageMeta({ layout: 'admin', middleware: 'auth' })
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

const load = async () => {
  loading.value = true
  error.value = null
  await loadTitles()
  try {
    data.value = await api<Page<AdminComment>>(
      `/admin/comments?status=${encodeURIComponent(status.value)}&per=${PER}&page=${page.value}`,
    )
  } catch (e: any) {
    error.value = e
  } finally {
    loading.value = false
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
          @click="((status = t.value), (page = 1), load())"
          >{{ t.label }}</el-button
        >
      </div>
    </div>

    <el-skeleton v-if="loading" :rows="6" animated />
    <ErrorBox v-else-if="error" :error="error.status === 403 ? { message: '需要管理员角色才能查看评论' } : error" :retry="error.status !== 403" @retry="load" />
    <p v-else-if="!data?.items.length" class="muted">这个分类下暂时没有评论。</p>
    <template v-else>
      <div v-for="c in data.items" :key="c.id" class="panel card">
        <div class="toolbar" style="margin-bottom: 6px">
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
        @current-change="(n) => { page = n; load() }"
        style="margin-top:16px;justify-content:center;display:flex"
      />
    </template>
  </section>
</template>
