<script setup lang="ts">
// 公开站点头部:站点名 + 导航(可在设置中自定义)+ 明暗切换
// 窄屏(≤768px)导航收入汉堡菜单,避免 header 文字挤压
import { Menu, Close } from '@element-plus/icons-vue'

const { site, settings } = useSettings()
const nav = computed(() =>
  (
    settings.value?.nav ?? [
      { label: '首页', href: '/' },
      { label: '分类', href: '/categories' },
      { label: '标签', href: '/tags' },
      { label: '搜索', href: '/search' },
    ]
  )
    .map((n) => ({ ...n, href: safeHref(n.href, true) }))
    .filter((n) => !!n.href),
)
const isExternalHref = (href: string) => /^https?:\/\//i.test(href)

const menuOpen = ref(false)
const searchOpen = ref(false)
const route = useRoute()
// 路由变化自动收起菜单
watch(
  () => route.fullPath,
  () => {
    menuOpen.value = false
  },
)

// 「搜索」不是普通跳转:直接弹出悬浮搜索框
const isSearch = (href: string) => href === '/search'
</script>

<template>
  <header class="site-header card">
    <div class="container header-inner">
      <NuxtLink class="brand" to="/">
        <h1 class="brand-title">{{ site.title || 'fanke' }}</h1>
        <h2 v-if="site.subtitle" class="brand-sub muted">—— {{ site.subtitle }}</h2>
      </NuxtLink>
      <nav class="nav nav-desktop">
        <template v-for="n in nav" :key="n.href">
          <button v-if="isSearch(n.href)" class="nav-btn" type="button" @click="searchOpen = true">{{ n.label }}</button>
          <a v-else-if="isExternalHref(n.href)" :href="n.href" target="_blank" rel="noopener noreferrer">{{ n.label }}</a>
          <NuxtLink v-else :to="n.href">{{ n.label }}</NuxtLink>
        </template>
      </nav>
      <ThemeToggle />
      <el-button class="menu-btn" :aria-expanded="menuOpen" aria-label="菜单" @click="menuOpen = !menuOpen">
        <el-icon><Menu v-if="!menuOpen" /><Close v-else /></el-icon>
      </el-button>
    </div>
    <nav v-if="menuOpen" class="nav nav-mobile">
      <template v-for="n in nav" :key="n.href">
        <button v-if="isSearch(n.href)" class="nav-btn" type="button" @click="menuOpen = false; searchOpen = true">{{ n.label }}</button>
        <a v-else-if="isExternalHref(n.href)" :href="n.href" target="_blank" rel="noopener noreferrer">{{ n.label }}</a>
        <NuxtLink v-else :to="n.href">{{ n.label }}</NuxtLink>
      </template>
    </nav>
  </header>
  <SearchPalette v-model="searchOpen" />
</template>

<style scoped>
.site-header {
  position: sticky;
  top: 12px;
  z-index: 50;
  margin: 12px auto 0;
  max-width: var(--maxw);
}
.header-inner {
  display: flex;
  align-items: center;
  gap: 20px;
  padding: 10px 20px;
}
.brand {
  display: flex;
  align-items: baseline;
  gap: 10px;
  color: var(--text);
}
.brand:hover {
  text-decoration: none;
}
.brand:hover .brand-title {
  color: var(--primary);
}
.brand-title {
  margin: 0;
  font-weight: 700;
  font-size: 20px;
  line-height: 1.4;
}
.brand-sub {
  margin: 0;
  font-size: 13px;
  font-weight: 400;
  line-height: 1.4;
}
.nav {
  display: flex;
  gap: 16px;
  margin-left: auto;
  flex-wrap: wrap;
}
.nav a {
  color: var(--muted);
  font-size: 15px;
  padding: 4px 10px;
  border-radius: 8px;
}
.nav a:hover {
  background: var(--primary-soft);
  color: var(--primary);
  text-decoration: none;
}
/* 搜索入口与导航链接完全同款,只是改成按钮触发浮层 */
.nav-btn {
  padding: 4px 10px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--muted);
  font-family: var(--font);
  font-size: 15px;
  cursor: pointer;
}
.nav-btn:hover {
  background: var(--primary-soft);
  color: var(--primary);
}
.menu-btn {
  display: none;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

/* 窄屏:平铺导航收进汉堡菜单 */
@media (max-width: 768px) {
  .header-inner {
    gap: 12px;
    padding: 10px 14px;
  }
  .brand {
    margin-right: auto;
  }
  .brand-title {
    font-size: 18px;
  }
  .brand-sub {
    display: none;
  }
  .nav-desktop {
    display: none;
  }
  .menu-btn {
    display: inline-flex;
  }
  .nav-mobile {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin: 0;
    padding: 4px 10px 10px;
    border-top: 1px solid var(--border);
  }
  .nav-mobile a {
    padding: 10px 12px;
    font-size: 15px;
  }
  .nav-mobile .nav-btn {
    padding: 10px 12px;
    text-align: left;
  }
}
@media (min-width: 769px) {
  .nav-mobile {
    display: none;
  }
}
</style>
