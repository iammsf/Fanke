<script setup lang="ts">
// 公开站点页脚;footer 支持 HTML/JS(管理员输入,如备案链接、统计脚本)
const { site } = useSettings()
const footerEl = ref<HTMLElement | null>(null)

// 版权信息:支持 $y(当前年份) $admin(管理员昵称);留空回退默认
const copyrightText = computed(() => {
  const y = String(new Date().getFullYear())
  const admin = site.value.admin_name || site.value.author || 'admin'
  const t = (site.value.copyright || '').trim()
  if (!t) return `© ${y} ${site.value.title || 'fanke'}`
  return t.replaceAll('$y', y).replaceAll('$admin', admin)
})

// 页脚脚本可能注册 setInterval(如站点运行时长),离开前台页面前必须清掉,
// 否则定时器会继续在后台等没有页脚元素的页面执行并报 null 错误。
const timerIds = new Set<number>()
const clearTimers = () => {
  for (const id of timerIds) window.clearInterval(id)
  timerIds.clear()
}
// 脚本同步执行,期间临时接管 setInterval/setTimeout 记录其定时器 ID
const captureTimers = (run: () => void) => {
  const setInterval = window.setInterval
  const setTimeout = window.setTimeout
  window.setInterval = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
    const id = setInterval.call(window, fn, ms, ...args)
    timerIds.add(id)
    return id
  }) as typeof window.setInterval
  window.setTimeout = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
    const id = setTimeout.call(window, fn, ms, ...args)
    timerIds.add(id)
    return id
  }) as typeof window.setTimeout
  try {
    run()
  } finally {
    window.setInterval = setInterval
    window.setTimeout = setTimeout
  }
}

// v-html 插入的 <script> 不会自动执行,手动重建 script 节点以生效
const runScripts = async () => {
  await nextTick()
  clearTimers()
  captureTimers(() => {
    footerEl.value?.querySelectorAll('script').forEach((old) => {
      const s = document.createElement('script')
      for (const a of old.attributes) s.setAttribute(a.name, a.value)
      s.textContent = old.textContent
      old.replaceWith(s)
    })
  })
}
watch(() => site.value.footer, runScripts)
onMounted(runScripts)
onBeforeUnmount(clearTimers)
</script>

<template>
  <footer class="site-footer">
    <div class="container">
      <div v-if="site.footer" ref="footerEl" v-html="site.footer" />
      <p v-else>{{ site.author || '' }} · 基于 fanke</p>
      <p>{{ copyrightText }}</p>
    </div>
  </footer>
</template>

<style scoped>
.site-footer {
  padding: 28px 0 40px;
  text-align: center;
  color: var(--muted);
  font-size: 14px;
  border-top: 1px solid var(--border);
  margin-top: 24px;
}
</style>
