<script setup lang="ts">
// 标签详情(与分类详情共用 TermPostList)
// key 固定:同一路由内切换标签时不重挂载页面,只刷新文章列表
definePageMeta({ key: 'tag-detail' })
const route = useRoute()
const { siteTitle, load } = useSettings()
load()
useHead({ title: computed(() => `标签 · ${siteTitle.value}`) })
</script>

<template>
  <TermPostList type="tag" :slug="route.params.slug as string" />
</template>
