<script setup lang="ts">
// 后台布局:左侧导航 + 右侧内容区,复用公开页设计令牌与明暗主题
import {
  Odometer,
  Document,
  Picture,
  ChatDotRound,
  Grid,
  MagicStick,
  User,
  Setting,
} from '@element-plus/icons-vue'

const { logout } = useAuth()
const { site, load: loadSettings } = useSettings()
const { apply: applyTheme } = useThemeCss()
const { me, load: loadMe } = useAdminMe()
const route = useRoute()

const links = [
  { id: 'dashboard', label: '仪表盘', href: '/admin', icon: Odometer },
  { id: 'posts', label: '文章', href: '/admin/posts', icon: Document },
  { id: 'media', label: '媒体', href: '/admin/media', icon: Picture },
  { id: 'comments', label: '评论', href: '/admin/comments', icon: ChatDotRound },
  { id: 'plugins', label: '插件', href: '/admin/plugins', icon: Grid },
  { id: 'ai-connections', label: 'AI 连接器', href: '/admin/ai-connections', icon: MagicStick },
  { id: 'users', label: '用户', href: '/admin/users', icon: User },
  { id: 'settings', label: '设置', href: '/admin/settings', icon: Setting },
]

const active = computed(() => {
  const p = route.path
  if (p === '/admin') return 'dashboard'
  const seg = p.split('/')[2] || ''
  return seg
})

// 非管理员只保留自己有权访问的入口,管理员专属页面(插件/AI/用户/设置)不显示
const visibleLinks = computed(() => {
  if (!me.value || me.value.role === 'admin') return links
  // 评论审核接口要求 admin,作者不可见该入口
  const authorAllowed = new Set(['dashboard', 'posts', 'media'])
  return links.filter((l) => authorAllowed.has(l.id))
})
onMounted(async () => {
  // 并行拉取:站点设置 + 账号信息 + 启用插件列表
  const { ensurePublic } = useExtensions()
  const [settings] = await Promise.all([loadSettings(), loadMe(), ensurePublic()])
  // 后台主题与文章页主题分开,只注入 admin 主题
  await applyTheme('admin', settings?.theme_admin || '')
})
</script>

<template>
  <div class="admin">
    <aside class="side card">
      <div class="side-head">
        <NuxtLink class="brand" to="/">{{ site.title || 'fanke' }}</NuxtLink>
        <span class="side-sub muted">管理后台</span>
      </div>
      <nav class="menu">
        <NuxtLink v-for="l in visibleLinks" :key="l.id" :to="l.href" :class="{ on: active === l.id }">
          <el-icon class="ico"><component :is="l.icon" /></el-icon>
          {{ l.label }}
        </NuxtLink>
      </nav>
      <div class="side-foot">
        <div class="muted">{{ me ? `${me.display_name || me.username}(${me.role === 'admin' ? '管理员' : me.role})` : '' }}</div>
        <div class="side-btns">
          <el-button size="small" tag="a" href="/" target="_blank">查看站点 ↗</el-button>
          <ThemeToggle />
          <el-button size="small" type="danger" @click="logout">退出</el-button>
        </div>
      </div>
    </aside>
    <main class="content">
      <slot />
    </main>
  </div>
</template>

<style scoped>
.admin {
  display: flex;
  gap: 20px;
  align-items: flex-start;
  width: 100%;
  max-width: none;
  margin: 0 auto;
  padding: 16px 12px 48px;
  min-height: 100vh;
}
.side {
  position: sticky;
  top: 16px;
  width: 240px;
  flex: none;
  padding: 18px 14px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.side-head {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 0 8px;
}
.side-head .brand {
  font-weight: 700;
  font-size: 19px;
  color: var(--text);
}
.side-head .brand:hover {
  color: var(--primary);
  text-decoration: none;
}
.side-sub {
  font-size: 12.5px;
}
.menu {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.menu a {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  border-radius: 10px;
  color: var(--muted);
  font-size: 15px;
}
.menu a:hover {
  background: var(--primary-soft);
  color: var(--primary);
  text-decoration: none;
}
.menu a.on {
  background: var(--primary);
  color: #fff;
}
.menu .ico {
  width: 18px;
  font-size: 17px;
  flex: none;
}
.side-foot {
  margin-top: auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 0 8px;
}
.side-btns {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}
.content {
  flex: 1;
  min-width: 0;
}
@media (max-width: 860px) {
  .admin {
    flex-direction: column;
    padding: 12px 12px 40px;
  }
  .side {
    position: static;
    width: auto;
    flex-direction: row;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
  }
  .side-head {
    flex-direction: row;
    align-items: baseline;
    gap: 8px;
    padding: 0;
  }
  .menu {
    flex-direction: row;
    flex-wrap: wrap;
  }
  .side-foot {
    margin-top: 0;
    margin-left: auto;
  }
}
</style>
