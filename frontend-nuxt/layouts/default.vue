<script setup lang="ts">
// 公开布局:头部/页脚 + 设置加载 + 运行时主题 + 用户插件注入
const { ensurePublic } = useExtensions()
if (import.meta.client) void ensurePublic()
useSiteBoot(true)
</script>

<template>
  <div>
    <AppHeader />
    <main class="container main-body">
      <slot />
    </main>
    <AppFooter />
  </div>
</template>

<style scoped>
.main-body {
  padding-top: 24px;
  padding-bottom: 48px;
  min-height: calc(100vh - 220px);
}
</style>
