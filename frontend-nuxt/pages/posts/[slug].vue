<script setup lang="ts">
// 文章详情:标题/元信息/封面/正文/分类标签/评论区
import type { Post } from '~/types'

const route = useRoute()
const api = usePublicApi()
const { siteTitle, load } = useSettings()
load()

const slug = route.params.slug as string
const post = ref<Post | null>(null)
const loading = ref(true)
const notFound = ref(false)
const error = ref<any>(null)

// image-viewer 插件:启用时转换正文中的图片说明标签
const iv = useImageViewer()
onMounted(() => iv.load())
const contentHtml = computed(() => iv.transform(post.value?.content_html || ''))

const articleEl = ref<HTMLElement>()

const cats = computed(() => post.value?.terms?.filter((t) => t.type === 'category') ?? [])
const tags = computed(() => post.value?.terms?.filter((t) => t.type === 'tag') ?? [])

// 图片预览:点正文图片,用 Element Plus 图片查看器浏览本文正文图片(可左右切换)
// 封面图不在放大范围内(用户要求),故点击只挂在 .prose 上,封面不参与
const viewerOpen = ref(false)
const viewerIndex = ref(0)
const viewerUrls = ref<string[]>([])
const openViewer = (e: MouseEvent) => {
  const target = e.target as HTMLElement
  if (target?.tagName !== 'IMG') return
  const scope = e.currentTarget as HTMLElement
  const imgs = Array.from(scope.querySelectorAll('img')).filter((i) => i.getAttribute('src')) as HTMLImageElement[]
  if (!imgs.length) return
  // 正文 <img> 的 src 已指向缩图端点,真正的大图地址在后端注入的 data-orig 里
  viewerUrls.value = imgs.map((i) => i.getAttribute('data-orig') || (i.getAttribute('src') as string))
  viewerIndex.value = Math.max(0, imgs.indexOf(target as HTMLImageElement))
  viewerOpen.value = true
}
const minutes = computed(() =>
  post.value ? readingMinutes((post.value.content_html || '').replace(/<[^>]+>/g, '')) : 1,
)

// 封面比例兜底:width/height 属性要等 srcset 候选选定后才参与比例计算,
// 首帧存在"图已插入但高度还没定"的瞬间,页脚会被推下去(Lighthouse 实测 CLS 0.153)。
// 直接用 CSS aspect-ratio 提前把占位高度钉死。
const coverRatio = computed(() => {
  const w = post.value?.cover_width || 0
  const h = post.value?.cover_height || 0
  return w && h ? { aspectRatio: `${w} / ${h}` } : undefined
})

const fetchData = async () => {
  loading.value = true
  notFound.value = false
  error.value = null
  const res = await api.post(slug)
  if (res.ok && res.data) {
    post.value = res.data.post
  } else if (res.status === 404) {
    notFound.value = true // 确实不存在,或未公开且当前未登录
  } else {
    // 网络错误 / 5xx:显示可重试的错误,而不是误报"文章不存在"
    error.value = new Error(res.status === 0 ? '网络连接失败' : `加载失败(${res.status})`)
  }
  loading.value = false
}
onMounted(fetchData)

const pageUrl = computed(() => {
  if (import.meta.client) return window.location.href
  return ''
})
const coverUrl = computed(() => post.value?.cover_url || '')

useHead({
  title: computed(() => (post.value ? `${post.value.title} · ${siteTitle.value}` : siteTitle.value)),
  meta: [
    { name: 'description', content: computed(() => post.value?.summary || '') },
    // 社交平台不执行 JS,必须在能执行时把 OG 补上(分享卡片依赖它)
    { property: 'og:type', content: 'article' },
    { property: 'og:title', content: computed(() => post.value?.title || siteTitle.value) },
    { property: 'og:description', content: computed(() => post.value?.summary || '') },
    { property: 'og:url', content: pageUrl },
    ...(coverUrl.value ? [{ property: 'og:image', content: coverUrl.value }] : []),
    { name: 'twitter:card', content: coverUrl.value ? 'summary_large_image' : 'summary' },
  ],
})
</script>

<template>
  <div>
    <!-- 骨架至少占满一屏:文章接口返回前页脚若落在视口内,内容一到就会把它推走(CLS 0.155 的元凶)。
         撑满 100vh 让页脚始终在视口外,位移不可见。 -->
    <div v-if="loading" class="post-sk">
      <el-skeleton :rows="10" animated />
    </div>
    <ErrorBox v-else-if="notFound" :error="{ message: '文章不存在或未公开' }" />
    <ErrorBox v-else-if="error" :error="error" retry @retry="fetchData" />
    <template v-else-if="post">
      <article ref="articleEl" class="post-full card">
        <h1 class="p-title">{{ post.title }}</h1>
        <p class="p-meta muted">
          {{ fmtDate(post.published_at || post.created_at) }} · {{ post.author_name }} · 约 {{ minutes }} 分钟 · 阅读
          {{ post.views }}
        </p>
        <p v-if="cats.length" class="p-terms">
          <AppTag v-for="t in cats" :key="t.id" :to="`/categories/${t.slug}`">{{ t.name }}</AppTag>
        </p>
        <img
          v-if="post.cover_url"
          class="cover"
          :data-orig="post.cover_url"
          :src="thumbUrl(post.cover_url, 1080)"
          :srcset="thumbSrcset(post.cover_url, [480, 740, 1080, 1440], post.cover_width || 0)"
          sizes="(max-width: 720px) 92vw, 1040px"
          :width="post.cover_width || undefined"
          :height="post.cover_height || undefined"
          :style="coverRatio"
          :alt="post.title"
          fetchpriority="high"
          decoding="async"
        />
        <div class="prose" v-html="contentHtml" @click="openViewer" />
        <p v-if="tags.length" class="p-terms tags">
          <AppTag v-for="t in tags" :key="t.id" :to="`/tags/${t.slug}`">#{{ t.name }}</AppTag>
        </p>
      </article>
      <CommentSection :slug="slug" :allow="post.allow_comments !== false" />
    </template>

    <!-- Element Plus 图片查看器:封面 + 正文内全部图片 -->
    <el-image-viewer
      v-if="viewerOpen"
      :url-list="viewerUrls"
      :initial-index="viewerIndex"
      teleported
      hide-on-click-modal
      @close="viewerOpen = false"
    />
  </div>
</template>

<style scoped>
.post-sk {
  min-height: 100vh;
}
.post-full {
  padding: 32px 40px 36px;
}
.p-title {
  margin: 0 0 10px;
  font-size: 28px;
  line-height: 1.35;
}
.p-meta {
  margin: 0 0 14px;
  font-size: 13.5px;
}
.p-terms {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin: 0 0 18px;
}
.p-terms.tags {
  margin: 22px 0 0;
}
.cover {
  width: 100%;
  height: auto; /* 让 aspect-ratio(来自 width/height 属性或内联 style)决定高度 */
  border-radius: 12px;
  margin: 0 0 20px;
}
/* 正文图片可点开预览(封面不放大,故不含 .cover) */
.prose :deep(img) {
  cursor: zoom-in;
}
@media (max-width: 640px) {
  .post-full {
    padding: 20px;
  }
}
</style>
