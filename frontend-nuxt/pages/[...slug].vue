<script setup lang="ts">
// 未匹配的任意路径:统一显示 404,而不是落到首页空壳。
// 注意:纯静态 SPA 无法返回真正的 404 状态码(由 CDN 直接托管 HTML),
// 这里保证用户侧不会看到空白页;爬虫侧的正确状态码需要服务端渲染或边缘规则。
const { siteTitle } = useSettings()
useHead({ title: computed(() => `页面不存在 · ${siteTitle.value}`) })
</script>

<template>
  <div class="nf card">
    <p class="code">404</p>
    <h1>这个页面不存在</h1>
    <p class="muted">链接可能已经失效,或者地址拼错了。</p>
    <NuxtLink class="back" to="/">← 返回首页</NuxtLink>
  </div>
</template>

<style scoped>
.nf {
  text-align: center;
  padding: 64px 24px;
}
.code {
  font-size: 64px;
  font-weight: 700;
  margin: 0;
  opacity: 0.25;
  letter-spacing: -0.04em;
}
h1 {
  font-size: 22px;
  margin: 8px 0 12px;
}
.back {
  display: inline-block;
  margin-top: 16px;
  text-decoration: none;
  color: var(--el-color-primary, #2563eb);
}
</style>
