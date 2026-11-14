<script setup lang="ts">
// 全局搜索浮层:悬浮输入框 + 相关性靠前的文章卡片,点击卡片直接跳转
import type { Page, PostSummary } from '~/types'
import { Loading, Picture, Search } from '@element-plus/icons-vue'

const open = defineModel<boolean>({ default: false })

const api = usePublicApi()
const router = useRouter()

const keyword = ref('')
const inputRef = ref<any>(null)
const loading = ref(false)
const error = ref<any>(null)
const data = ref<Page<PostSummary> | null>(null)
let timer: ReturnType<typeof setTimeout> | null = null
let seq = 0

const results = computed(() => data.value?.items ?? [])
const total = computed(() => data.value?.total ?? 0)
const kw = computed(() => keyword.value.trim())

// 键盘高亮项:↑↓ 切换,Enter 打开当前项
const active = ref(0)
const listRef = ref<HTMLElement | null>(null)

const close = () => {
  open.value = false
}

const scrollActive = () => {
  nextTick(() => {
    listRef.value?.querySelector<HTMLElement>('.sp-item.is-active')?.scrollIntoView({ block: 'nearest' })
  })
}

const setActive = (i: number) => {
  active.value = i
  scrollActive()
}

const move = (delta: number) => {
  const n = results.value.length
  if (!n) return
  setActive((active.value + delta + n) % n)
}

watch(results, () => {
  active.value = 0
})

watch(open, (v) => {
  document.body.style.overflow = v ? 'hidden' : ''
  if (!v) return
  nextTick(() => {
    inputRef.value?.focus?.()
    inputRef.value?.select?.()
  })
})

// 输入防抖 220ms,只采用最后一次请求的结果
watch(keyword, () => {
  if (timer) clearTimeout(timer)
  if (!kw.value) {
    seq++
    data.value = null
    error.value = null
    loading.value = false
    return
  }
  loading.value = true
  const my = ++seq
  timer = setTimeout(async () => {
    const res = await api.posts({ q: kw.value, per: 6, page: 1 })
    if (my !== seq) return
    if (res) {
      data.value = res
      error.value = null
    } else {
      data.value = null
      error.value = new Error('搜索服务暂不可用')
    }
    loading.value = false
  }, 220)
})

const goPost = (p: PostSummary) => {
  close()
  router.push(`/posts/${p.slug}`)
}

const goAll = () => {
  if (!kw.value) return
  const q = kw.value
  close()
  router.push({ path: '/search', query: { q } })
}

const onEnter = () => {
  const item = results.value[active.value]
  if (item) goPost(item)
  else goAll()
}

const dateText = (v?: string | null) => {
  if (!v) return ''
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString('zh-CN')
}

const esc = (s: string) =>
  s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!)

const hl = (text: string) => {
  const q = kw.value
  if (!q) return esc(text)
  const re = new RegExp(`(${q.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`, 'gi')
  return esc(text).replace(re, '<mark>$1</mark>')
}

const onKey = (e: KeyboardEvent) => {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
    e.preventDefault()
    open.value = true
  } else if (e.key === 'Escape' && open.value) {
    close()
  }
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  document.body.style.overflow = ''
})
</script>

<template>
  <Teleport to="body">
    <Transition name="sp">
      <div v-if="open" class="sp-mask" @click.self="close">
        <div class="sp-panel" role="dialog" aria-modal="true" aria-label="搜索文章">
          <el-input
            ref="inputRef"
            v-model="keyword"
            class="sp-input"
            size="large"
            clearable
            placeholder="搜索文章标题、内容、标签…"
            @keydown.down.prevent="move(1)"
            @keydown.up.prevent="move(-1)"
            @keydown.enter.prevent="onEnter"
          >
            <template #prefix>
              <el-icon><Search /></el-icon>
            </template>
            <template #suffix>
              <el-icon v-if="loading" class="sp-spin"><Loading /></el-icon>
            </template>
          </el-input>

          <div class="sp-body">
            <p v-if="!kw" class="sp-tip muted">输入关键词，下方显示相关性靠前的文章；回车打开第一篇。</p>
            <p v-else-if="error && !results.length" class="sp-tip muted">{{ error.message }}</p>
            <template v-else-if="results.length">
              <div ref="listRef" class="sp-list" role="listbox">
                <button
                  v-for="(p, i) in results"
                  :key="p.id"
                  class="sp-item"
                  :class="{ 'is-active': i === active }"
                  type="button"
                  role="option"
                  :aria-selected="i === active"
                  @mouseenter="setActive(i)"
                  @click="goPost(p)"
                >
                  <span class="sp-thumb">
                    <img v-if="p.cover_url" :src="p.cover_url" alt="" loading="lazy" />
                    <el-icon v-else class="sp-thumb-empty"><Picture /></el-icon>
                  </span>
                  <span class="sp-main">
                    <span class="sp-title" :title="p.title" v-html="hl(p.title)" />
                    <span v-if="p.summary" class="sp-sub" v-html="hl(p.summary)" />
                    <span class="sp-meta">{{ dateText(p.published_at || p.created_at) }}<template v-if="p.views"> · {{ p.views }} 次阅读</template></span>
                  </span>
                </button>
              </div>
              <div class="sp-more">
                <el-button link type="primary" @click="goAll">查看全部 {{ total }} 条结果</el-button>
              </div>
            </template>
            <p v-else-if="loading" class="sp-tip muted">搜索中…</p>
            <p v-else class="sp-tip muted">没有找到相关文章</p>
          </div>

          <div class="sp-foot">
            <span><kbd>↑</kbd><kbd>↓</kbd> 选择</span>
            <span><kbd>↵</kbd> 打开</span>
            <span><kbd>Esc</kbd> 关闭</span>
            <span v-if="results.length" class="sp-count">共 {{ total }} 条</span>
            <span v-else class="sp-count">Ctrl / ⌘ + K 唤起</span>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.sp-mask {
  position: fixed;
  inset: 0;
  z-index: 2000;
  display: flex;
  justify-content: center;
  align-items: flex-start;
  padding: 12vh 16px 24px;
  background: rgba(15, 23, 42, 0.42);
  backdrop-filter: blur(4px);
}
.sp-panel {
  width: 100%;
  max-width: 640px;
  padding: 14px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 16px;
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.22);
}
.sp-input :deep(.el-input__wrapper) {
  border-radius: 999px;
  padding-left: 16px;
  padding-right: 14px;
}
.sp-input :deep(.el-input__inner) {
  font-size: 15px;
}
.sp-spin {
  animation: sp-spin 0.9s linear infinite;
}
@keyframes sp-spin {
  to {
    transform: rotate(360deg);
  }
}
.sp-body {
  margin-top: 10px;
  max-height: 52vh;
  overflow-y: auto;
}
.sp-list {
  display: flex;
  flex-direction: column;
}
.sp-tip {
  margin: 0;
  padding: 18px 6px;
  text-align: center;
  font-size: 14px;
}
.sp-item {
  display: flex;
  gap: 12px;
  align-items: center;
  width: 100%;
  padding: 10px 12px;
  border: 0;
  border-radius: 10px;
  background: transparent;
  text-align: left;
  cursor: pointer;
  font-family: var(--font);
  transition: background 0.12s ease;
}
.sp-thumb {
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: 88px;
  height: 56px;
  border-radius: 8px;
  overflow: hidden;
  background: var(--surface-2);
  color: var(--muted);
}
.sp-thumb img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.sp-thumb-empty {
  font-size: 18px;
}
.sp-main {
  display: block;
  flex: 1;
  min-width: 0;
}
.sp-item:hover,
.sp-item:focus-visible,
.sp-item.is-active {
  background: var(--primary-soft);
  outline: none;
}
.sp-title {
  display: block;
  font-size: 15px;
  font-weight: 600;
  color: var(--text);
  line-height: 1.5;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.sp-sub {
  display: block;
  margin-top: 3px;
  font-size: 13px;
  color: var(--muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sp-meta {
  display: block;
  margin-top: 4px;
  font-size: 12px;
  color: var(--muted);
}
.sp-item :deep(mark) {
  padding: 0 1px;
  border-radius: 3px;
  background: rgba(255, 203, 63, 0.45);
  color: inherit;
}
.sp-more {
  padding: 6px 6px 2px;
  text-align: right;
}
.sp-foot {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 8px;
  padding: 9px 8px 1px;
  border-top: 1px solid var(--border);
  color: var(--muted);
  font-size: 12px;
}
.sp-foot kbd {
  display: inline-block;
  min-width: 18px;
  margin-right: 2px;
  padding: 1px 5px;
  border: 1px solid var(--border);
  border-bottom-width: 2px;
  border-radius: 4px;
  background: var(--surface-2);
  font-family: var(--mono);
  font-size: 11px;
  text-align: center;
}
.sp-count {
  margin-left: auto;
}
.sp-enter-active,
.sp-leave-active {
  transition: opacity 0.16s ease;
}
.sp-enter-active .sp-panel,
.sp-leave-active .sp-panel {
  transition: transform 0.18s ease;
}
.sp-enter-from,
.sp-leave-to {
  opacity: 0;
}
.sp-enter-from .sp-panel,
.sp-leave-to .sp-panel {
  transform: translateY(-8px) scale(0.99);
}
@media (max-width: 640px) {
  .sp-mask {
    padding: 8vh 12px 16px;
  }
}
@media (prefers-reduced-motion: reduce) {
  .sp-enter-active,
  .sp-leave-active,
  .sp-enter-active .sp-panel,
  .sp-leave-active .sp-panel {
    transition: none;
  }
}
</style>
