<script setup lang="ts">
// 文章列表:骨架/错误/空态/卡片/分页 全状态封装(首页/分类/标签/搜索共用)
import type { Page, PostSummary } from '~/types'

withDefaults(
  defineProps<{
    data: Page<PostSummary> | null
    loading: boolean
    error?: any
    page: number
    emptyText?: string
  }>(),
  { emptyText: '还没有文章。' },
)
const emit = defineEmits<{ go: [page: number]; retry: [] }>()

// 骨架占位数量:取满一页(默认 10 条),使骨架列表的总高度≈加载后的真实列表。
// 矮骨架会让页脚在加载完成的瞬间被推出视口 —— 这正是 Lighthouse 报的那次 0.153 CLS
// (首页 76 分的最大扣分项)。数量对齐后,页脚在骨架期就已在视口之外,不会再发生位移。
const SKELETON_COUNT = 10
// 首屏(约 3 张卡片高度)直接 eager + 高优先级加载:这些图就是 LCP 候选,
// 用 preload 提前下载图片字节,显示仍等待插件状态与焦点定位就绪,保证首次出现的位置正确。
const EAGER_CARDS = 3
</script>

<template>
  <!-- 骨架必须与真实卡片的盒子一致(同 16:9 比例、同下边距),否则加载完成瞬间页面高度剧变 -->
  <div v-if="loading" class="sk-list" aria-hidden="true">
    <div v-for="i in SKELETON_COUNT" :key="i" class="sk-card card sk-shine" />
  </div>
  <ErrorBox v-else-if="error" :error="error" retry @retry="emit('retry')" />
  <template v-else-if="data">
    <p v-if="!data.items.length" class="muted">{{ emptyText }}</p>
    <PostCard
      v-for="(p, i) in data.items"
      :key="p.id"
      :post="p"
      :priority="i < EAGER_CARDS"
    />
    <el-pagination
      background
      hide-on-single-page
      layout="total, prev, pager, next, jumper"
      :page-size="data.per || 10"
      :total="data.total"
      :current-page="page"
      @current-change="emit('go', $event)"
      style="margin-top:16px;justify-content:center;display:flex"
    />
  </template>
</template>

<style scoped>
.sk-list {
  display: block;
}
.sk-card {
  aspect-ratio: 16 / 9;
  padding: 0;
  margin-bottom: 18px;
  border-radius: var(--radius, 10px);
}
/* 骨架闪光:与 Element Plus 骨架同一组令牌与曲线,保证观感统一 */
.sk-shine {
  background: linear-gradient(
    90deg,
    var(--el-fill-color, #f2f3f5) 25%,
    var(--el-fill-color-darker, #e6e8eb) 37%,
    var(--el-fill-color, #f2f3f5) 63%
  );
  background-size: 400% 100%;
  animation: sk-shine 1.4s ease infinite;
}
@keyframes sk-shine {
  0% {
    background-position: 100% 50%;
  }
  100% {
    background-position: 0 50%;
  }
}
@media (max-width: 640px) {
  /* 移动端卡片是 120×213 定高封面,骨架同尺寸才能保证零位移 */
  .sk-card {
    aspect-ratio: auto;
    height: 213px;
  }
}
@media (prefers-reduced-motion: reduce) {
  .sk-shine {
    animation: none;
  }
}
</style>
