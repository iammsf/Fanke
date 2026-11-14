<script setup lang="ts">
// 明暗切换按钮(公开页头部与后台侧栏共用)
import { Moon, Sunny } from '@element-plus/icons-vue'

const { toggle } = useTheme()
const dark = ref(false)

onMounted(() => {
  dark.value = document.documentElement.dataset.theme === 'dark'
})

const onToggle = () => {
  toggle()
  dark.value = document.documentElement.dataset.theme === 'dark'
}
</script>

<template>
  <el-button circle class="toggle" :aria-label="dark ? '切换到明亮模式' : '切换到暗黑模式'" @click="onToggle">
    <el-icon><Moon v-if="!dark" /><Sunny v-else /></el-icon>
  </el-button>
</template>
