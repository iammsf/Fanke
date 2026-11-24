<script setup lang="ts">
// 首页:副标题 + 文章列表(分页走 URL query)
import type { Page, PostSummary } from '~/types'

const api = usePublicApi()
const route = useRoute()
const router = useRouter()
const { site, siteTitle, load } = useSettings()
load()

const data = ref<Page<PostSummary> | null>(null)
const loading = ref(true)
const error = ref<any>(null)
const page = computed(() => Math.max(1, Number(route.query.page) || 1))

let seq = 0

const fetchData = async () => {
  const my = ++seq
  loading.value = true
  error.value = null
  const res = await api.posts({ page: page.value, per: 10 })
  if (my !== seq) return
  if (res) data.value = res
  else error.value = new Error('加载失败')
  loading.value = false
}

const go = (p: number) => router.push({ query: { page: p } })
watch(() => route.query.page, () => {
  fetchData()
  window.scrollTo({ top: 0 })
})
onMounted(fetchData)

useHead({
  title: computed(() => siteTitle.value),
  meta: [
    { name: 'description', content: computed(() => site.value.subtitle || '') },
    { property: 'og:type', content: 'website' },
    { property: 'og:title', content: computed(() => siteTitle.value) },
    { property: 'og:description', content: computed(() => site.value.subtitle || '') },
    { name: 'twitter:card', content: 'summary' },
  ],
})
</script>

<template>
  <div>
    <PostList :data="data" :loading="loading" :error="error" :page="page" @go="go" @retry="fetchData" />
  </div>
</template>

<style scoped>
</style>
