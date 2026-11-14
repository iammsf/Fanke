<script setup lang="ts">
// 新建文章(与编辑共用 PostEditor)
definePageMeta({ layout: 'admin', middleware: 'auth' })
useHead({ title: '新文章 · fanke 后台' })
</script>

<template>
  <PostEditor />
</template>
