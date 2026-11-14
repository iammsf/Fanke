<script setup lang="ts">
// 分类详情(与标签详情共用 TermPostList)
// key 固定:同一路由内切换分类时不重挂载页面,只刷新文章列表
definePageMeta({ key: 'category-detail' })
const route = useRoute()
const { siteTitle, load } = useSettings()
load()
useHead({ title: computed(() => `分类 · ${siteTitle.value}`) })
</script>

<template>
  <TermPostList type="category" :slug="route.params.slug as string" />
</template>
