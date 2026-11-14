<script setup lang="ts">
// 编辑文章(与新建共用 PostEditor)
definePageMeta({ layout: 'admin', middleware: 'auth' })

const route = useRoute()
const raw = String(route.params.id ?? '')
const id = /^\d+$/.test(raw) ? Number(raw) : 0

useHead({ title: '编辑文章 · fanke 后台' })
</script>

<template>
  <PostEditor v-if="id > 0" :post-id="id" />
  <ErrorBox v-else :error="{ message: '无效的文章 ID' }" />
</template>
