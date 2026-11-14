<script setup lang="ts">
// 分类/标签总览云(两个页面共用,仅 type 不同)
const props = defineProps<{ type: 'category' | 'tag' }>()

const { terms, ensure } = useTermCache(props.type)
const loading = ref(!terms.value.length)
const error = ref<any>(null)

const load = async () => {
  loading.value = !terms.value.length
  error.value = null
  const res = await ensure(true)
  if (res === null) error.value = new Error('加载失败')
  loading.value = false
}
onMounted(load)
</script>

<template>
  <el-skeleton v-if="loading" :rows="3" animated />
  <ErrorBox v-else-if="error" :error="error" retry @retry="load" />
  <div v-else class="cloud">
    <AppTag v-for="t in terms" :key="t.id" :to="`/${type === 'category' ? 'categories' : 'tags'}/${t.slug}`" class="cloud-tag">
      {{ type === 'tag' ? '#' : '' }}{{ t.name }} <small>({{ t.count }})</small>
    </AppTag>
  </div>
</template>

<style scoped>
.cloud {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
.cloud :deep(.cloud-tag) {
  font-size: 15px;
  padding: 6px 16px;
}
</style>
