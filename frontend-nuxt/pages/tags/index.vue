<script setup lang="ts">
// 全部标签(与分类总览共用 TermCloud)
const { siteTitle, load } = useSettings()
load()
useHead({ title: computed(() => `标签 · ${siteTitle.value}`) })
</script>

<template>
  <div>
    <h1 class="page-title">全部标签</h1>
    <TermCloud type="tag" />
  </div>
</template>
