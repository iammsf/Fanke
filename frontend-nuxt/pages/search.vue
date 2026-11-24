<script setup lang="ts">
// 搜索结果页:关键词走 URL query,列表复用 PostList(快速搜索见全局浮层 SearchPalette)
import type { Page, PostSummary } from '~/types'
import { Search } from '@element-plus/icons-vue'

const api = usePublicApi()
const route = useRoute()
const router = useRouter()
const { siteTitle, load } = useSettings()
load()

const q = computed(() => String(route.query.q || ''))
const page = computed(() => Math.max(1, Number(route.query.page) || 1))
const input = ref(q.value)

const data = ref<Page<PostSummary> | null>(null)
const loading = ref(false)
const error = ref<any>(null)

let seq = 0

const fetchData = async () => {
  if (!q.value) {
    seq++
    data.value = null
    error.value = null
    loading.value = false
    return
  }
  const my = ++seq
  loading.value = true
  error.value = null
  const res = await api.posts({ q: q.value, page: page.value, per: 10 })
  if (my !== seq) return
  if (res) data.value = res
  else error.value = new Error('搜索服务暂不可用')
  loading.value = false
}

const search = () => {
  const v = input.value.trim()
  if (v) router.push({ query: { q: v } })
}
const go = (p: number) => router.push({ query: { q: q.value, page: p } })

watch(
  () => [route.query.q, route.query.page],
  () => {
    input.value = q.value
    fetchData()
    window.scrollTo({ top: 0 })
  },
)
onMounted(fetchData)

useHead({ title: computed(() => `搜索 · ${siteTitle.value}`) })
</script>

<template>
  <div>
    <form class="sform" @submit.prevent="search">
      <el-input v-model="input" size="large" clearable placeholder="输入关键词后回车…" class="sinput">
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>
      <el-button type="primary" size="large" native-type="submit">搜索</el-button>
    </form>
    <template v-if="q">
      <p class="muted">{{ data ? `找到 ${data.total} 篇` : loading ? '搜索中…' : '搜索服务暂不可用' }}</p>
      <PostList :data="data" :loading="loading" :error="error" :page="page" @go="go" @retry="fetchData" />
    </template>
    <p v-else class="muted">输入关键词开始搜索，或按 Ctrl / ⌘ + K 打开快速搜索。</p>
  </div>
</template>

<style scoped>
.sform {
  display: flex;
  gap: 10px;
  margin-bottom: 20px;
  max-width: 640px;
}
.sinput {
  flex: 1;
}
.sinput :deep(.el-input__wrapper) {
  border-radius: 999px;
  padding-left: 16px;
}
</style>
