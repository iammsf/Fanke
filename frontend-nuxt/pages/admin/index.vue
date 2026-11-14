<script setup lang="ts">
// 仪表盘:管理员看站点概览与快捷入口;非管理员只看自己的资料与密码
definePageMeta({ layout: 'admin', middleware: 'auth' })
import type { AdminPost, CoreUpdateInfo, Page } from '~/types'
import { DocumentAdd, Picture, ChatDotRound, Setting, Refresh, Link } from '@element-plus/icons-vue'

const { api, logout } = useAuth()
const { push } = useToast()
const { run } = useBusy()

const loading = ref(true)
const loadError = ref<any>(null)
const { me, load: loadMe } = useAdminMe()
const stats = ref({ posts: '-', drafts: '-', pending: '-' })
const host = ref('')
const version = ref('')
const coreUpdate = ref<CoreUpdateInfo | null>(null)
const updateBusy = ref(false)
const updateCheckedAt = ref<number | null>(null)

const UPDATE_CACHE_KEY = 'fanke:core-update'

const isAdmin = computed(() => me.value?.role === 'admin')

const loadVersion = async () => {
  try {
    version.value = (await api<{ version: string }>('/version'))?.version || ''
  } catch {
    // 版本接口失败不影响仪表盘
  }
}

// 检查结果本地持久化:切换页面/刷新后仍能显示上次结果,不用重复点检查。
const loadCachedUpdate = () => {
  try {
    const raw = localStorage.getItem(UPDATE_CACHE_KEY)
    if (!raw) return
    const saved = JSON.parse(raw) as { result?: CoreUpdateInfo; checkedAt?: number }
    if (!saved?.result || !saved.checkedAt) return
    // 升级过(版本变化)后旧结果不可信,直接丢弃
    if (version.value && saved.result.current && saved.result.current !== version.value) return
    coreUpdate.value = saved.result
    updateCheckedAt.value = saved.checkedAt
  } catch {
    // 缓存损坏时忽略
  }
}

const saveCachedUpdate = () => {
  if (!coreUpdate.value) return
  updateCheckedAt.value = Date.now()
  try {
    localStorage.setItem(
      UPDATE_CACHE_KEY,
      JSON.stringify({ result: coreUpdate.value, checkedAt: updateCheckedAt.value }),
    )
  } catch {
    // 隐私模式等无法写入时忽略
  }
}

const checkUpdate = async () => {
  updateBusy.value = true
  try {
    coreUpdate.value = await run('检查更新中…', () => api<CoreUpdateInfo>('/admin/update/check'))
    saveCachedUpdate()
    push(
      coreUpdate.value.has_update ? `有新版本 ${coreUpdate.value.latest}` : '已是最新版本',
      !coreUpdate.value.has_update,
    )
  } catch (e: any) {
    push(e.message || '检查更新失败', false)
  } finally {
    updateBusy.value = false
  }
}

const openRelease = () => {
  const url = coreUpdate.value?.page_url
  if (url) window.open(url, '_blank', 'noopener')
}

const loadData = async () => {
  loading.value = true
  loadError.value = null
  try {
    const user = await loadMe()
    if (!user) throw new Error('无法获取账号信息')
    if (user.role !== 'admin') {
      loading.value = false
      return
    }
    // 各统计单独容错:author 角色访问评论接口会 403,显示 "-"
    const [posts, drafts, pending] = await Promise.all([
      api<Page<AdminPost>>('/admin/posts?per=1').catch(() => null),
      api<Page<AdminPost>>('/admin/posts?status=draft&per=1').catch(() => null),
      api<Page<unknown>>('/admin/comments?status=pending&per=1').catch(() => null),
    ])
    stats.value = {
      posts: posts ? String(posts.total) : '-',
      drafts: drafts ? String(drafts.total) : '-',
      pending: pending ? String(pending.total) : '-',
    }
    if (!posts && !drafts) push('统计数据加载失败', false)
  } catch (e: any) {
    loadError.value = e
  } finally {
    loading.value = false
  }
}
onMounted(() => {
  host.value = location.host
  // 版本与统计并行拉取;拿到版本后再恢复上次的检查结果
  loadVersion().then(loadCachedUpdate)
  loadData()
})

// ---- 个人资料 ----
const profile = reactive({ display_name: '', email: '', avatar_url: '', bio: '' })
watch(me, (m) => {
  if (!m) return
  profile.display_name = m.display_name || ''
  profile.email = m.email || ''
  profile.avatar_url = m.avatar_url || ''
  profile.bio = m.bio || ''
})
const saveProfile = async () => {
  try {
    await run('保存中…', () => api('/auth/me', {
      method: 'PUT',
      body: JSON.stringify({
        email: profile.email.trim(),
        display_name: profile.display_name.trim(),
        avatar_url: profile.avatar_url.trim(),
        bio: profile.bio,
      }),
    }))
    push('资料已保存', true)
  } catch (e: any) {
    push(e.message || '保存失败', false)
  }
}

// ---- 修改密码 ----
const pw = reactive({ current: '', next: '', confirm: '' })
const pwBusy = ref(false)
const savePw = async () => {
  if (pwBusy.value) return
  if (!pw.current) return push('请输入当前密码', false)
  if (pw.next.length < 10) return push('新密码至少 10 位', false)
  if (pw.next !== pw.confirm) return push('两次输入的新密码不一致', false)
  pwBusy.value = true
  try {
    await run('更新中…', () => api('/auth/password', { method: 'POST', body: JSON.stringify({ current: pw.current, password: pw.next }) }))
    pw.current = ''
    pw.next = ''
    pw.confirm = ''
    push('密码已更新,即将退出,请用新密码重新登录', true)
    setTimeout(() => logout(), 1500) // 后端已吊销全部会话
  } catch (e: any) {
    push(e.message || '更新失败', false)
    pwBusy.value = false
  }
}

useHead({ title: '仪表盘 · fanke 后台' })
</script>

<template>
  <div class="dashboard">
    <el-skeleton v-if="loading" :rows="4" animated />
    <ErrorBox v-else-if="loadError" :error="loadError" retry @retry="loadData" />
    <template v-else-if="me">
      <div class="panel card">
        <h2>你好,{{ me.display_name || me.username }}</h2>
        <p class="muted">
          角色:{{ me.role === 'admin' ? '管理员' : '作者' }} · 注册于 {{ fmtDate(me.created_at) }} · 站点:{{ host }}
        </p>
      </div>

      <template v-if="isAdmin">
        <div class="stat-row">
          <NuxtLink to="/admin/posts" style="display: block; text-decoration: none">
            <div class="stat card"><b>{{ stats.posts }}</b><span>文章总数</span></div>
          </NuxtLink>
          <NuxtLink to="/admin/posts?status=draft" style="display: block; text-decoration: none">
            <div class="stat card"><b>{{ stats.drafts }}</b><span>草稿</span></div>
          </NuxtLink>
          <NuxtLink to="/admin/comments" style="display: block; text-decoration: none">
            <div class="stat card"><b>{{ stats.pending }}</b><span>待审评论</span></div>
          </NuxtLink>
        </div>

        <div class="panel card">
          <h2>快捷入口</h2>
          <div class="quick-actions">
            <el-button type="primary" :icon="DocumentAdd" @click="navigateTo('/admin/posts/new')">写新文章</el-button>
            <el-button :icon="Picture" @click="navigateTo('/admin/media')">上传媒体</el-button>
            <el-button :icon="ChatDotRound" @click="navigateTo('/admin/comments')">审核评论</el-button>
            <el-button :icon="Setting" @click="navigateTo('/admin/settings')">站点设置</el-button>
          </div>
        </div>

        <div class="panel card update-card">
          <h2>版本与更新</h2>
          <dl class="update-meta">
            <div>
              <dt>当前版本</dt>
              <dd>{{ version || '未知' }}</dd>
            </div>
            <div>
              <dt>最新版本</dt>
              <dd v-if="coreUpdate">
                <template v-if="coreUpdate.has_update">
                  {{ coreUpdate.latest }}<span v-if="coreUpdate.date" class="update-date"> · {{ coreUpdate.date }}</span>
                </template>
                <span v-else>已是最新</span>
              </dd>
              <dd v-else><span class="update-none">未检查</span></dd>
            </div>
          </dl>
          <div class="update-actions">
            <el-button type="primary" :icon="Refresh" :loading="updateBusy" @click="checkUpdate">检查更新</el-button>
            <el-button
              v-if="coreUpdate?.has_update && coreUpdate.page_url"
              :icon="Link"
              @click="openRelease"
            >查看发布页</el-button>
          </div>
          <p v-if="updateCheckedAt" class="update-checked">
            上次检查 {{ fmtDateTime(new Date(updateCheckedAt).toISOString()) }}
          </p>
          <template v-if="coreUpdate?.has_update">
            <p v-if="coreUpdate.notes" class="update-notes">{{ coreUpdate.notes }}</p>
            <p class="muted update-tip">
              升级命令 <code>sudo bash deploy/install.sh --update</code>，只更新后端与前端，
              <code>extensions/</code> 插件目录、主题与媒体数据都会保留。
            </p>
          </template>
        </div>
      </template>

      <div v-else class="panel card account-card">
        <el-form class="account-section" label-position="top" @submit.prevent="saveProfile">
          <h2>个人资料</h2>
          <div class="profile-fields">
            <el-form-item label="显示昵称"><el-input v-model="profile.display_name" /></el-form-item>
            <el-form-item label="邮箱"><el-input v-model="profile.email" type="email" /></el-form-item>
          </div>
          <el-form-item label="头像"><el-input v-model="profile.avatar_url" placeholder="头像 URL(可选)" /></el-form-item>
          <el-form-item label="简介"><el-input v-model="profile.bio" type="textarea" :rows="2" /></el-form-item>
          <el-button type="primary" native-type="submit">保存资料</el-button>
        </el-form>

        <el-form class="account-section password-section" label-position="top" autocomplete="off" @submit.prevent="savePw">
          <h2>修改密码</h2>
          <input type="text" name="username" autocomplete="username" style="display:none" :value="me?.username" />
          <el-form-item label="当前密码"><el-input v-model="pw.current" type="password" show-password placeholder="当前密码" autocomplete="current-password" /></el-form-item>
          <el-form-item label="新密码"><el-input v-model="pw.next" type="password" show-password placeholder="新密码(≥10 位)" autocomplete="new-password" /></el-form-item>
          <el-form-item label="确认新密码"><el-input v-model="pw.confirm" type="password" show-password placeholder="再次输入新密码" autocomplete="new-password" /></el-form-item>
          <el-button type="primary" native-type="submit" :disabled="pwBusy">更新密码</el-button>
        </el-form>
      </div>
    </template>
  </div>
</template>

<style scoped>
.dashboard {
  container-type: inline-size;
  container-name: dashboard;
}
.quick-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}
.update-card h2 {
  margin-bottom: 16px;
}
.update-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 12px 48px;
  margin: 0 0 16px;
}
.update-meta dt {
  margin: 0 0 3px;
  font-size: 12px;
  color: var(--muted);
}
.update-meta dd {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  line-height: 1.4;
  font-variant-numeric: tabular-nums;
}
.update-date {
  font-size: 12px;
  font-weight: 400;
  color: var(--muted);
}
.update-none {
  font-size: 13px;
  font-weight: 400;
  color: var(--muted);
}
.update-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.update-checked {
  margin: 10px 0 0;
  font-size: 12px;
  color: var(--muted);
}
.update-notes {
  margin: 14px 0 0;
  padding: 10px 12px;
  border-radius: 6px;
  background: var(--el-fill-color-light, #f5f7fa);
  white-space: pre-wrap;
  font-size: 13px;
  line-height: 1.7;
  color: var(--text);
}
.update-tip {
  margin: 8px 0 0;
  font-size: 12.5px;
  line-height: 1.7;
}
.update-tip code {
  padding: 1px 4px;
  border-radius: 4px;
  background: var(--el-fill-color-light, #f5f7fa);
  font-size: 12px;
}
.account-card {
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr);
  gap: 24px;
}
.account-section {
  min-width: 0;
  width: 100%;
  box-sizing: border-box;
}
.password-section {
  border-left: 1px solid var(--border);
  padding-left: 24px;
  box-sizing: border-box;
}
.profile-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}
.account-card :deep(.el-form-item) {
  min-width: 0;
  margin-bottom: 12px;
}
.account-card :deep(.el-input),
.account-card :deep(.el-textarea) {
  width: 100%;
}
@media (max-width: 900px) {
  .account-card {
    grid-template-columns: minmax(0, 1fr);
    gap: 20px;
  }
  .password-section {
    border-left: 0;
    border-top: 1px solid var(--border);
    padding-left: 0;
    padding-top: 20px;
  }
}
@container dashboard (max-width: 760px) {
  .account-card {
    grid-template-columns: minmax(0, 1fr);
    gap: 20px;
  }
  .password-section {
    border-left: 0;
    border-top: 1px solid var(--border);
    padding-left: 0;
    padding-top: 20px;
  }
}
@container dashboard (max-width: 480px) {
  .account-card {
    padding: 16px;
    gap: 16px;
  }
  .profile-fields {
    grid-template-columns: minmax(0, 1fr);
    gap: 0;
  }
  .password-section {
    padding-top: 16px;
  }
  .account-section > .el-button {
    width: 100%;
    margin-left: 0;
  }
}
@media (max-width: 480px) {
  .account-card {
    padding: 16px;
    gap: 16px;
  }
  .profile-fields {
    grid-template-columns: minmax(0, 1fr);
    gap: 0;
  }
  .password-section {
    padding-top: 16px;
  }
  .account-section > .el-button {
    width: 100%;
    margin-left: 0;
  }
}
</style>
