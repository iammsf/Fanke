<script setup lang="ts">
// 文章卡片(首页/分类/标签/搜索列表共用)
// 桌面端:封面全幅,悬停时蒙版自底部升起(保留卡面上浮/阴影动画)
// 移动端:左图右文,内容始终可见
import type { PostSummary } from '~/types'
// 显式导入:auto-import 仅覆盖 Nuxt 约定目录,utils 里的常量在模板中不会被注入,
// 直接写在模板上会被编译成 e.COVER_SIZES → 运行时 undefined,sizes 属性整条丢失。
import { COVER_SIZES, COVER_WIDTHS } from '~/utils/thumb'

const props = defineProps<{ post: PostSummary; priority?: boolean }>()
const cats = computed(() => props.post.terms?.filter((t) => t.type === 'category') ?? [])
const tags = computed(() => props.post.terms?.filter((t) => t.type === 'tag') ?? [])
</script>

<template>
  <article class="post-card card" :class="{ 'no-cover': !post.cover_url }">
    <NuxtLink v-if="post.cover_url" class="cover-link" :to="`/posts/${post.slug}`">
      <FocusImg
        img-class="cover"
        :src="post.cover_url"
        :alt="post.title"
        :widths="COVER_WIDTHS"
        :sizes="COVER_SIZES"
        :width="post.cover_width"
        :height="post.cover_height"
        :priority="priority"
      />
    </NuxtLink>
    <div class="mask">
      <h3 class="m-title">
        <NuxtLink class="stretched" :to="`/posts/${post.slug}`">{{ post.title }}</NuxtLink>
      </h3>
      <h4 v-if="post.summary" class="m-summary">{{ post.summary }}</h4>
      <h5 class="m-meta">
        <span class="m-date">{{ fmtDate(post.published_at || post.created_at) }}</span>
        <span class="m-tags">
          <AppTag v-for="c in cats" :key="`c${c.id}`" :to="`/categories/${c.slug}`">{{ c.name }}</AppTag>
          <AppTag v-for="t in tags" :key="`t${t.id}`" :to="`/tags/${t.slug}`">#{{ t.name }}</AppTag>
        </span>
        <span class="m-views">{{ post.views }} 阅读</span>
      </h5>
    </div>
  </article>
</template>

<style scoped>
/* ---------- 桌面端:封面全幅 + 悬停蒙版 ---------- */
.post-card {
  position: relative;
  overflow: hidden;
  aspect-ratio: 16 / 9; /* 桌面端封面 16:9 */
  padding: 0;
  margin-bottom: 18px;
  transition: transform 0.15s, box-shadow 0.15s;
}
.post-card:hover {
  transform: translateY(-2px);
}
.no-cover {
  background: linear-gradient(135deg, #3a4a6b 0%, #1f2733 100%);
}
.cover-link {
  position: absolute;
  inset: 0;
  display: block;
}
.cover {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  transition: transform 0.35s ease;
}
.post-card:hover .cover {
  transform: scale(1.04);
}
.mask {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  padding: 18px 22px 16px;
  background: rgba(10, 13, 18, 0.78);
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
  color: #f2f4f8;
  opacity: 0;
  transform: translateY(100%);
  transition: transform 0.28s ease, opacity 0.22s ease;
}
.post-card:hover .mask,
.no-cover .mask {
  opacity: 1;
  transform: translateY(0);
}
.m-title {
  margin: 0 0 6px;
  font-size: 20px;
  line-height: 1.35;
  display: -webkit-box;
  -webkit-line-clamp: 1;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.m-title a {
  color: inherit;
}
.m-title a:hover {
  text-decoration: none;
  opacity: 0.85;
}
/* 整卡可点:标题链接拉伸覆盖全卡;标签浮于其上来保持可点 */
.stretched::after {
  content: '';
  position: absolute;
  inset: 0;
}
.m-summary {
  margin: 0 0 8px;
  font-size: 14px;
  font-weight: 400;
  line-height: 1.55;
  color: rgba(242, 244, 248, 0.78);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.m-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin: 0;
  padding-right: 74px; /* 给右下角阅读量留位 */
  font-size: 12.5px;
  font-weight: 400;
  color: rgba(242, 244, 248, 0.6);
}
.m-meta :deep(a) {
  position: relative;
  z-index: 2;
}
/* 桌面端标签与日期同一行排列 */
.m-tags {
  display: contents;
}
.m-tags :deep(.tag) {
  font-size: 12.5px;
  margin: 0;
}
.m-views {
  position: absolute;
  right: 22px;
  bottom: 16px;
  font-size: 12.5px;
  color: rgba(242, 244, 248, 0.6);
  white-space: nowrap;
}

/* ---------- 移动端:左图右文,内容常显 ---------- */
@media (max-width: 640px) {
  .post-card {
    display: flex;
    height: auto; /* 高度由 9:16 封面决定 */
  }
  /* 无封面卡片:移动端蒙版透明+深色文字,去掉深色渐变底防止不可读 */
  .no-cover {
    background: none;
  }
  /* 封面容器定死 120×213(9:16):宽高都写死,不依赖 aspect-ratio,旧浏览器也生效 */
  .cover-link {
    position: relative;
    width: 120px;
    height: 213px;
    flex-shrink: 0;
    align-self: flex-start;
    overflow: hidden;
  }
  /* .cover 沿用桌面端样式:position:absolute; inset:0; 100% 填充; object-fit:cover */
  .mask {
    position: static;
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    opacity: 1;
    transform: none;
    background: transparent;
    backdrop-filter: none;
    -webkit-backdrop-filter: none;
    color: var(--text);
    padding: 12px 14px;
    overflow: hidden;
  }
  .m-title {
    font-size: 16px;
    -webkit-line-clamp: 1;
    margin-bottom: 6px;
  }
  .m-summary {
    font-size: 12.5px;
    color: var(--muted);
    -webkit-line-clamp: 2;
    margin-bottom: 6px;
  }
  /* meta 钉底:日期/标签/阅读量位置全站一致 */
  .m-meta {
    flex-wrap: wrap;
    align-items: flex-end;
    gap: 4px 8px;
    padding-right: 0;
    margin: auto 0 0;
    font-size: 11.5px;
    color: var(--muted);
  }
  /* 日期独占第一行 */
  .m-date {
    flex: 0 0 100%;
  }
  /* 标签第二行,最多两行;阅读量紧随其右 */
  .m-tags {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    max-height: 52px; /* 芯片约24px,两行完整显示 */
    overflow: hidden;
  }
  /* 移动端 meta 内字号统一(日期/标签/阅读量均 11.5px) */
  .m-tags :deep(.tag) {
    font-size: 11.5px;
    margin: 0;
  }
  .m-views {
    position: static;
    margin-left: auto;
    flex-shrink: 0;
    font-size: 11.5px;
    color: var(--muted);
    white-space: nowrap;
  }
}
</style>
