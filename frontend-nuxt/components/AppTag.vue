<script setup lang="ts">
// 标签/分类小胶囊;to 存在时为链接
defineProps<{ to?: string }>()
</script>

<template>
  <NuxtLink v-if="to" :to="to" class="tag"><slot /></NuxtLink>
  <span v-else class="tag"><slot /></span>
</template>
