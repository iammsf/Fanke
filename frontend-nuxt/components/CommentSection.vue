<script setup lang="ts">
// 评论区:列表 + 提交表单(文章详情页使用)
import type { CommentItem } from '~/types'

const props = defineProps<{ slug: string; allow: boolean }>()

const api = usePublicApi()
const items = ref<CommentItem[]>([])
const loading = ref(true)
const error = ref<any>(null)
// 评论开关以后端响应为准(全局策略 × 文章开关)
const allowForm = ref(false)

// 表单状态
const form = reactive({ author_name: '', author_email: '', content: '', website: '' })
const status = ref('')
const submitting = ref(false)

const load = async () => {
  loading.value = true
  error.value = null
  if (!props.allow) {
    items.value = []
    allowForm.value = false
    loading.value = false
    return
  }
  const res = await api.comments(props.slug)
  if (res) {
    items.value = res.items
    allowForm.value = res.allow_comments !== false
  } else {
    error.value = new Error('评论加载失败')
  }
  loading.value = false
}
onMounted(load)

const submit = async () => {
  if (submitting.value) return
  submitting.value = true
  status.value = '提交中…'
  try {
    const base = (useRuntimeConfig().public.apiBase || '/api/v1').replace(/\/+$/, '')
    const headers: Record<string, string> = { 'Content-Type': 'application/json' }
    const token = sessionStorage.getItem('fanke_access')
    if (token) headers.Authorization = `Bearer ${token}`
    const res = await fetch(`${base}/posts/${encodeURIComponent(props.slug)}/comments`, {
      method: 'POST',
      headers,
      body: JSON.stringify(form),
    })
    if (res.status === 201) {
      status.value = '已发表'
      form.content = ''
      load() // 不整页刷新,直接重新拉取评论列表
    } else if (res.status === 202) {
      status.value = '已提交,等待博主审核'
      form.content = ''
    } else if (res.status === 429) {
      status.value = '手速太快,稍后再来'
    } else {
      const j = await res.json().catch(() => ({}))
      status.value = '失败:' + (j.error || res.status)
    }
  } catch {
    status.value = '网络错误'
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <section id="comments" class="comments card">
    <h3>评论({{ items.length }})</h3>
    <el-skeleton v-if="loading" :rows="6" animated />
    <ErrorBox v-else-if="error" :error="error" retry @retry="load" />
    <template v-else>
      <p v-if="!items.length" class="muted">还没有评论,来坐沙发。</p>
      <ul class="clist">
        <li v-for="c in items" :key="c.id" class="citem">
          <div class="cmeta muted"><strong>{{ c.author_name }}</strong> · {{ fmtDate(c.created_at) }}</div>
          <div class="ctext prose" v-html="c.content" />
        </li>
      </ul>
    </template>

    <form v-if="allowForm" class="cform" autocomplete="off" @submit.prevent="submit">
      <input v-model="form.website" type="text" name="website" class="hp" tabindex="-1" aria-hidden="true" />
      <div class="row">
        <el-input v-model="form.author_name" placeholder="昵称 *" maxlength="60" required />
        <el-input v-model="form.author_email" type="email" placeholder="邮箱(选填,不公开)" maxlength="120" />
      </div>
      <el-input
        v-model="form.content"
        type="textarea"
        placeholder="友善的评论(2-4000 字) *"
        :autosize="{ minRows: 3, maxRows: 12 }"
        required
        maxlength="4000"
      />
      <div class="row end">
        <span class="status muted">{{ status }}</span>
        <el-button type="primary" native-type="submit" :disabled="submitting">发表评论</el-button>
      </div>
    </form>
  </section>
</template>

<style scoped>
.comments {
  margin-top: 32px;
  padding: 22px 24px;
}
.comments h3 {
  margin: 0 0 14px;
}
.clist {
  list-style: none;
  padding: 0;
  margin: 0;
}
.citem {
  padding: 12px 0;
  border-top: 1px solid var(--border);
}
.cmeta {
  margin-bottom: 4px;
}
.cform {
  margin-top: 18px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.cform .hp {
  position: absolute;
  left: -9999px;
}
.row {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}
.row .el-input {
  flex: 1;
  min-width: 220px;
}
.row.end {
  justify-content: flex-end;
  align-items: center;
}
</style>
