<script setup lang="ts">
// 全部分类(与标签总览共用 TermCloud)
const { siteTitle, load } = useSettings()
load()
useHead({ title: computed(() => `分类 · ${siteTitle.value}`) })
</script>

<template>
  <div>
    <h1 class="page-title">全部分类</h1>
    <TermCloud type="category" />
  </div>
</template>
