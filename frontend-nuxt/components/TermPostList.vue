<script setup lang="ts">
// 分类/标签详情文章列表(两个路由共用,仅 type 不同)
import type { Page, PostSummary } from '~/types'

const props = defineProps<{ type: 'category' | 'tag'; slug: string }>()

const api = usePublicApi()
const route = useRoute()
const router = useRouter()
// 术语列表与总览页共用缓存:从总览点进来时标题和胶囊立即就位,不会先塌成骨架
const { terms, ensure: ensureTerms } = useTermCache(props.type)

const data = ref<Page<PostSummary> | null>(null)
const loading = ref(true)
// 切换分类/标签时保留上一份列表,只做淡出+禁用点击,不整块换骨架屏
const switching = ref(false)
const error = ref<any>(null)

const page = computed(() => Math.max(1, Number(route.query.page) || 1))
const name = computed(() => terms.value.find((t) => t.slug === props.slug)?.name || props.slug)
let seq = 0

// 顶部标题与切换胶囊只加载一次;切换标签/分类时只刷新下方文章列表,
// 标题、胶囊和 header 都不重挂载,避免点击瞬间闪烁跳动。
const loadTerms = async () => {
  await ensureTerms(false)
}

const loadPosts = async () => {
  const my = ++seq
  switching.value = !!data.value
  loading.value = true
  error.value = null
  const res = props.type === 'category'
    ? await api.posts({ page: page.value, per: 10, category: props.slug })
    : await api.posts({ page: page.value, per: 10, tag: props.slug })
  if (my !== seq) return // 连点切换时丢弃过期结果
  if (res) data.value = res
  else error.value = new Error('加载失败')
  loading.value = false
  switching.value = false
}

const load = async () => {
  await Promise.all([loadTerms(), loadPosts()])
}

const go = (p: number) => {
  router.push({ query: { ...route.query, page: p } })
}

// 切换标签/分类只刷新下方列表(标题、胶囊与 header 不重挂载);
// 仅在翻页时回到顶部,切换术语时保持当前滚动位置。
watch([() => props.slug, page], ([slug, p], [oldSlug, oldPage]) => {
  if (slug === oldSlug && p === oldPage) return
  loadPosts()
  if (p !== oldPage) window.scrollTo({ top: 0 })
})
onMounted(load)
</script>

<template>
  <h1 class="page-title">{{ type === 'category' ? `分类:${name}` : `#${name}` }}</h1>
  <!-- 同类型 term 云:与总览页保持完全相同的胶囊尺寸与字号 -->
  <div v-if="terms.length" class="term-switch">
    <AppTag
      v-for="t in terms"
      :key="t.id"
      :to="`/${type === 'category' ? 'categories' : 'tags'}/${t.slug}`"
      class="cloud-tag"
      :class="{ active: t.slug === slug }"
    >
      {{ type === 'tag' ? '#' : '' }}{{ t.name }} <small>({{ t.count }})</small>
    </AppTag>
  </div>
  <div class="term-list" :class="{ switching }">
    <PostList
      :data="data"
      :loading="loading && !data"
      :error="error"
      :page="page"
      :empty-text="`这个${type === 'category' ? '分类' : '标签'}下暂时没有文章。`"
      @go="go"
      @retry="load"
    />
  </div>
</template>

<style scoped>
.term-list {
  transition: opacity 0.15s ease;
}
/* 切换术语时不闪骨架:保留旧列表并稍微变淡,直到新数据到达 */
.term-list.switching {
  opacity: 0.45;
  pointer-events: none;
}
.term-switch {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin: 0 0 18px;
}
.term-switch :deep(.cloud-tag) {
  font-size: 15px;
  padding: 6px 16px;
}
.term-switch :deep(.tag.active) {
  background: var(--primary);
  color: #fff;
}
.term-switch :deep(.tag.active small) {
  color: rgba(255, 255, 255, 0.75);
}
</style>
