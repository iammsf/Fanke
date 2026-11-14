<script setup lang="ts">
// 错误态:加载失败给出明确原因 + 可选重试,禁止永远停在"加载中"
defineProps<{ error: any; retry?: boolean }>()
const emit = defineEmits<{ retry: [] }>()
</script>

<template>
  <div class="panel card error-box">
    <el-alert :title="errText(error)" type="error" :closable="false" show-icon />
    <el-button v-if="retry" type="primary" style="margin-top: 16px" @click="emit('retry')">重试</el-button>
  </div>
</template>

<style scoped>
.error-box {
  text-align: center;
  padding: 40px 20px;
}
</style>
