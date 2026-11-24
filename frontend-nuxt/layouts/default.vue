<script setup lang="ts">
// 公开布局:头部/页脚 + 设置加载 + 运行时主题 + 用户插件注入
const { ensurePublic } = useExtensions()
const { clear: clearTheme } = useThemeCss()
if (import.meta.client) void ensurePublic()
useSiteBoot(true)
onBeforeUnmount(() => clearTheme('site'))
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
