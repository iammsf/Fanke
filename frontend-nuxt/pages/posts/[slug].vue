<script setup lang="ts">
// 文章详情:标题/元信息/封面/正文/分类标签/阅读进度/浮动目录/邻篇导航/评论区
import type { Post, PostNavItem } from '~/types'

interface TocItem {
  id: string
  text: string
  level: 2 | 3
}

const route = useRoute()
const api = usePublicApi()
const { siteTitle, load } = useSettings()
load()

const slug = computed(() => route.params.slug as string)
const post = ref<Post | null>(null)
const prevPost = ref<PostNavItem | null>(null)
const nextPost = ref<PostNavItem | null>(null)
const loading = ref(true)
const notFound = ref(false)
const error = ref<any>(null)

// image-viewer 插件:启用时转换正文中的图片说明标签
const iv = useImageViewer()
// 插件停用/卸载/文件损坏时,正文里它声明过的标签按安装时登记的模板回落渲染
const fb = useTagFallbacks()
onMounted(() => {
  iv.load()
  fb.load()
})
const contentHtml = computed(() => iv.transform(fb.render(post.value?.content_html || '')))

const articleEl = ref<HTMLElement>()

const cats = computed(() => post.value?.terms?.filter((t) => t.type === 'category') ?? [])
const tags = computed(() => post.value?.terms?.filter((t) => t.type === 'tag') ?? [])

// 阅读进度条 (0~100)
const progress = ref(0)
let scrollRaf = 0
const updateProgress = () => {
  scrollRaf = 0
  if (!import.meta.client || !articleEl.value) {
    progress.value = 0
    return
  }
  const rect = articleEl.value.getBoundingClientRect()
  const total = articleEl.value.offsetHeight - window.innerHeight
  if (total <= 0) {
    progress.value = rect.top <= 0 ? 100 : 0
    return
  }
  const passed = -rect.top
  progress.value = Math.max(0, Math.min(100, (passed / total) * 100))
}
const onScroll = () => {
  if (scrollRaf) return
  scrollRaf = window.requestAnimationFrame(updateProgress)
}

// 桌面端右侧浮动目录 (TOC)
const tocItems = ref<TocItem[]>([])
const activeHeadingId = ref('')
let headingObserver: IntersectionObserver | null = null

const clearToc = () => {
  headingObserver?.disconnect()
  headingObserver = null
  tocItems.value = []
  activeHeadingId.value = ''
}

const rebuildToc = async () => {
  if (!import.meta.client) return
  await nextTick()
  clearToc()
  const prose = articleEl.value?.querySelector('.prose')
  if (!prose) return

  const nodes = Array.from(prose.querySelectorAll('h2, h3')) as HTMLElement[]
  const usedIds = new Set<string>()
  const items: TocItem[] = []
  const observedEls: HTMLElement[] = []

  nodes.forEach((el, idx) => {
    const text = (el.textContent || '').trim()
    if (!text) return
    let id = (el.getAttribute('id') || '').trim()
    if (!id || usedIds.has(id)) {
      let candidate = `heading-${idx}`
      let suffix = 1
      while (usedIds.has(candidate)) {
        candidate = `heading-${idx}-${suffix++}`
      }
      id = candidate
      el.id = id
    }
    usedIds.add(id)
    items.push({
      id,
      text,
      level: el.tagName === 'H3' ? 3 : 2,
    })
    observedEls.push(el)
  })

  if (items.length < 2) return
  tocItems.value = items
  activeHeadingId.value = items[0].id

  if ('IntersectionObserver' in window) {
    headingObserver = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting && entry.target.id) {
            activeHeadingId.value = entry.target.id
            break
          }
        }
      },
      { rootMargin: '-84px 0px -68% 0px', threshold: 0 },
    )
    observedEls.forEach((el) => headingObserver?.observe(el))
  }
}

const scrollToHeading = (id: string) => {
  if (!import.meta.client) return
  const target = document.getElementById(id)
  if (!target) return
  const reduceMotion = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
  target.scrollIntoView({ behavior: reduceMotion ? 'auto' : 'smooth', block: 'start' })
  activeHeadingId.value = id
}

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

let fetchSeq = 0
const fetchData = async () => {
  const currentSeq = ++fetchSeq
  const currentSlug = slug.value
  loading.value = true
  notFound.value = false
  error.value = null
  post.value = null
  prevPost.value = null
  nextPost.value = null
  progress.value = 0
  clearToc()

  const res = await api.post(currentSlug)
  if (currentSeq !== fetchSeq) return

  if (res.ok && res.data) {
    post.value = res.data.post
    prevPost.value = res.data.prev ?? null
    nextPost.value = res.data.next ?? null
  } else if (res.status === 404) {
    notFound.value = true // 确实不存在,或未公开且当前未登录
  } else {
    // 网络错误 / 5xx:显示可重试的错误,而不是误报"文章不存在"
    error.value = new Error(res.status === 0 ? '网络连接失败' : `加载失败(${res.status})`)
  }
  loading.value = false
  if (post.value && import.meta.client) {
    await rebuildToc()
    updateProgress()
  }
}

onMounted(() => {
  window.addEventListener('scroll', onScroll, { passive: true })
  window.addEventListener('resize', onScroll, { passive: true })
  void fetchData()
})

watch(slug, (newSlug, oldSlug) => {
  if (newSlug === oldSlug) return
  if (import.meta.client) {
    const reduceMotion = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
    window.scrollTo({ top: 0, behavior: reduceMotion ? 'auto' : 'smooth' })
  }
  void fetchData()
})

watch(contentHtml, () => {
  if (post.value && import.meta.client) {
    void rebuildToc()
  }
})

onBeforeUnmount(() => {
  if (import.meta.client) {
    window.removeEventListener('scroll', onScroll)
    window.removeEventListener('resize', onScroll)
    if (scrollRaf) window.cancelAnimationFrame(scrollRaf)
  }
  clearToc()
})

const pageUrl = computed(() => {
  if (import.meta.client) return window.location.href
  return ''
})
const coverUrl = computed(() => post.value?.cover_url || '')

useHead(() => ({
  title: post.value ? `${post.value.title} · ${siteTitle.value}` : siteTitle.value,
  meta: [
    { name: 'description', content: post.value?.summary || '' },
    // 社交平台不执行 JS,必须在能执行时把 OG 补上(分享卡片依赖它)
    { property: 'og:type', content: 'article' },
    { property: 'og:title', content: post.value?.title || siteTitle.value },
    { property: 'og:description', content: post.value?.summary || '' },
    { property: 'og:url', content: pageUrl.value },
    ...(coverUrl.value ? [{ property: 'og:image', content: coverUrl.value }] : []),
    { name: 'twitter:card', content: coverUrl.value ? 'summary_large_image' : 'summary' },
  ],
}))
</script>

<template>
  <div>
    <div v-if="post" class="reading-progress" :style="{ width: `${progress}%` }" aria-hidden="true" />
    <!-- 骨架至少占满一屏:文章接口返回前页脚若落在视口内,内容一到就会把它推走(CLS 0.155 的元凶)。
         撑满 100vh 让页脚始终在视口外,位移不可见。 -->
    <div v-if="loading" class="post-sk">
      <el-skeleton :rows="10" animated />
    </div>
    <ErrorBox v-else-if="notFound" :error="{ message: '文章不存在或未公开' }" />
    <ErrorBox v-else-if="error" :error="error" retry @retry="fetchData" />
    <template v-else-if="post">
      <div class="post-layout">
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

        <aside v-if="tocItems.length >= 2" class="post-toc" aria-label="文章目录">
          <div class="post-toc-title">目录</div>
          <ul class="post-toc-list">
            <li v-for="item in tocItems" :key="item.id">
              <a
                :href="`#${item.id}`"
                class="post-toc-link"
                :class="{ 'is-sub': item.level === 3, 'is-active': activeHeadingId === item.id }"
                @click.prevent="scrollToHeading(item.id)"
              >
                {{ item.text }}
              </a>
            </li>
          </ul>
        </aside>
      </div>

      <nav v-if="prevPost || nextPost" class="post-nav" aria-label="文章导航">
        <NuxtLink v-if="prevPost" :to="`/posts/${prevPost.slug}`" class="post-nav-item card post-nav-prev">
          <span class="post-nav-label">← 上一篇</span>
          <span class="post-nav-title">{{ prevPost.title }}</span>
        </NuxtLink>
        <NuxtLink v-if="nextPost" :to="`/posts/${nextPost.slug}`" class="post-nav-item card post-nav-next">
          <span class="post-nav-label">下一篇 →</span>
          <span class="post-nav-title">{{ nextPost.title }}</span>
        </NuxtLink>
      </nav>

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
.reading-progress {
  position: fixed;
  top: 0;
  left: 0;
  height: 2px;
  background: var(--primary);
  z-index: 90;
  pointer-events: none;
  transition: width 0.08s linear;
}
.post-sk {
  min-height: 100vh;
}
.post-layout {
  position: relative;
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

.post-toc {
  display: none;
}
@media (min-width: 1520px) {
  .post-toc {
    display: block;
    position: fixed;
    top: 96px;
    left: calc(50% + 544px);
    width: 220px;
    max-height: calc(100vh - 130px);
    overflow-y: auto;
    padding: 12px 14px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    box-shadow: var(--shadow);
    z-index: 20;
  }
}
.post-toc-title {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--muted);
  margin-bottom: 8px;
}
.post-toc-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.post-toc-link {
  display: block;
  padding: 4px 8px;
  font-size: 13px;
  line-height: 1.45;
  color: var(--muted);
  border-left: 2px solid transparent;
  border-radius: 0 6px 6px 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.post-toc-link.is-sub {
  padding-left: 18px;
  font-size: 12.5px;
}
.post-toc-link.is-active {
  color: var(--primary);
  border-left-color: var(--primary);
  background: var(--primary-soft);
  font-weight: 500;
}

.post-nav {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
  margin-top: 22px;
}
.post-nav-item {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 16px 20px;
  text-decoration: none;
  color: var(--text);
  transition: border-color 0.2s, transform 0.2s;
}
.post-nav-item:hover {
  border-color: var(--primary);
  text-decoration: none;
}
.post-nav-next {
  grid-column: 2;
  text-align: right;
}
.post-nav-label {
  font-size: 12.5px;
  color: var(--muted);
}
.post-nav-title {
  font-size: 15px;
  font-weight: 600;
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

@media (max-width: 640px) {
  .post-full {
    padding: 20px;
  }
  .post-nav {
    grid-template-columns: 1fr;
  }
  .post-nav-next {
    grid-column: 1;
    text-align: left;
  }
}
</style>
