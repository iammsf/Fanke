<script setup lang="ts">
// WP 风格安装/找回向导:
//  - 全新部署(未配置数据库):第一步填数据库连接,第二步建站点+管理员。
//  - 已部署但数据库断连:第一步需出示服务器配置文件里的 RECOVERY_KEY,连回原库即找回全部数据。
//  - 正常已安装:提示并跳登录。
// 页面在后端不可达时也能渲染(状态由浏览器端拉取)。

type Stage = 'checking' | 'unreachable' | 'installed' | 'db' | 'admin' | 'recovered'

const stage = ref<Stage>('checking')
const recovery = ref(false)
const { siteTitle, load } = useSettings()
load()
const base = (useRuntimeConfig().public.apiBase || '/api/v1').replace(/\/+$/, '')

const boot = async () => {
  stage.value = 'checking'
  let s: any = null
  try {
    const res = await fetch(`${base}/setup/status`, { signal: AbortSignal.timeout(8000) })
    s = await res.json()
  } catch {
    s = null
  }
  if (!s) {
    stage.value = 'unreachable'
    return
  }
  if (s.state === 'ok' && !s.needs_setup) {
    stage.value = 'installed'
    return
  }
  if (s.state === 'ok' && s.needs_setup) {
    stage.value = 'admin' // 数据库已配好但库是空的:直接进第二步
    return
  }
  recovery.value = s.state === 'broken'
  stage.value = 'db'
}
onMounted(boot)

// ---- 第一步:数据库连接 ----
const db = reactive({ host: '', port: '', dbname: '', user: '', password: '', sslmode: '', recovery_key: '' })
const dbStatus = ref('')
const dbBusy = ref(false)

const submitDb = async () => {
  dbBusy.value = true
  dbStatus.value = '正在连接…'
  try {
    const res = await fetch(`${base}/setup/db`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...db }),
    })
    const j = await res.json().catch(() => ({}))
    if (!res.ok) {
      dbStatus.value = '失败:' + (j.error || res.status)
      dbBusy.value = false
      return
    }
    if (j.needs_setup) stage.value = 'admin'
    else stage.value = 'recovered'
  } catch {
    dbStatus.value = '网络错误'
    dbBusy.value = false
  }
}

// ---- 第二步:站点信息 + 管理员账号 ----
const adm = reactive({ title: '', subtitle: '', username: '', display_name: '', email: '', password: '', password2: '' })
const admStatus = ref('')
const admBusy = ref(false)

const submitAdmin = async () => {
  if (adm.password !== adm.password2) {
    admStatus.value = '两次密码不一致'
    return
  }
  admBusy.value = true
  admStatus.value = '正在初始化…'
  try {
    const res = await fetch(`${base}/setup`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        site: { title: adm.title, subtitle: adm.subtitle },
        admin: { username: adm.username, email: adm.email, password: adm.password, display_name: adm.display_name },
      }),
    })
    const j = await res.json().catch(() => ({}))
    if (!res.ok) {
      admStatus.value = '失败:' + (j.error || res.status)
      admBusy.value = false
      return
    }
    const { setToken } = useAuth()
    setToken(j.access_token)
    location.href = '/admin'
  } catch {
    admStatus.value = '网络错误'
    admBusy.value = false
  }
}

useHead({ title: computed(() => `初始化安装 · ${siteTitle.value}`) })
</script>

<template>
  <div class="setup card">
    <p v-if="stage === 'checking'" class="muted">正在检测安装状态…</p>

    <template v-else-if="stage === 'unreachable'">
      <h1>无法连接后端</h1>
      <p class="muted">API 服务未启动或不可达。请确认后端进程在运行后重试。</p>
      <div class="row"><el-button type="primary" @click="boot">重新检测</el-button></div>
    </template>

    <template v-else-if="stage === 'installed'">
      <h1>已安装</h1>
      <p class="muted">系统已完成配置,无需重复初始化。<NuxtLink to="/admin/login">前往登录</NuxtLink></p>
    </template>

    <template v-else-if="stage === 'recovered'">
      <h1>数据已找回</h1>
      <p class="muted">已连回原有数据库(检测到既有用户,账号密码不变)。</p>
      <div class="row"><el-button type="primary" @click="navigateTo('/admin/login')">前往登录</el-button></div>
    </template>

    <template v-else-if="stage === 'db'">
      <h1>{{ recovery ? '找回你的站点' : '欢迎使用 fanke' }}</h1>
      <p class="muted">
        {{
          recovery
            ? '检测到数据库连接已断开。重新填写数据库信息并出示服务器上的恢复密钥,即可连回原有数据(文章、用户、设置都在库里,不会重建)。'
            : '第 1 步(共 2 步):配置数据库连接。需要已建好的 PostgreSQL 数据库。'
        }}
      </p>
      <el-form label-position="top" autocomplete="off" @submit.prevent="submitDb">
        <el-form-item label="数据库主机 *"><el-input v-model="db.host" maxlength="120" placeholder="如 127.0.0.1" required /></el-form-item>
        <el-form-item label="端口"><el-input v-model="db.port" maxlength="5" placeholder="5432" /></el-form-item>
        <el-form-item label="数据库名 *"><el-input v-model="db.dbname" maxlength="63" placeholder="如 fanke" required /></el-form-item>
        <el-form-item label="数据库用户 *"><el-input v-model="db.user" maxlength="63" required /></el-form-item>
        <el-form-item label="数据库密码"><el-input v-model="db.password" type="password" show-password maxlength="200" /></el-form-item>
        <el-form-item label="SSL 模式"><el-input v-model="db.sslmode" maxlength="20" placeholder="disable(默认)" /></el-form-item>
        <template v-if="recovery">
          <el-form-item label="恢复密钥 *">
            <el-input v-model="db.recovery_key" type="password" show-password maxlength="128" required />
            <div class="hint" style="font-size:12px;color:var(--muted);margin-top:4px">在服务器配置文件(默认 /opt/fanke/state/fanke.env)中查看 RECOVERY_KEY。</div>
          </el-form-item>
        </template>
        <div class="row">
          <span class="status muted">{{ dbStatus }}</span>
          <el-button type="primary" native-type="submit" :disabled="dbBusy">测试并保存</el-button>
        </div>
      </el-form>
    </template>

    <template v-else-if="stage === 'admin'">
      <h1>欢迎使用 fanke</h1>
      <p class="muted">第 2 步(共 2 步):设置站点与管理员账号,完成后自动登录后台。</p>
      <el-form label-position="top" autocomplete="off" @submit.prevent="submitAdmin">
        <h2>站点信息</h2>
        <el-form-item label="站点标题 *"><el-input v-model="adm.title" maxlength="80" placeholder="我的博客" required /></el-form-item>
        <el-form-item label="副标题"><el-input v-model="adm.subtitle" maxlength="160" placeholder="随便写点什么" /></el-form-item>
        <h2>管理员账号</h2>
        <el-form-item label="登录 ID *(唯一,用于登录,不可重复)">
          <el-input v-model="adm.username" maxlength="40" placeholder="字母/数字/-/_,2-40 位" required pattern="[A-Za-z0-9_\-]{2,40}" />
        </el-form-item>
        <el-form-item label="昵称(展示名称,可与他人重复,不用于登录)">
          <el-input v-model="adm.display_name" maxlength="60" placeholder="默认与登录 ID 相同" />
        </el-form-item>
        <el-form-item label="邮箱 *"><el-input v-model="adm.email" type="email" maxlength="120" required /></el-form-item>
        <el-form-item label="密码 *"><el-input v-model="adm.password" type="password" show-password minlength="10" maxlength="200" placeholder="至少 10 位" required /></el-form-item>
        <el-form-item label="确认密码 *"><el-input v-model="adm.password2" type="password" show-password maxlength="200" required /></el-form-item>
        <div class="row">
          <span class="status muted">{{ admStatus }}</span>
          <el-button type="primary" native-type="submit" :disabled="admBusy">完成安装</el-button>
        </div>
      </el-form>
    </template>
  </div>
</template>

<style scoped>
.setup {
  max-width: 560px;
  margin: 40px auto;
  padding: 30px 34px;
}
h1 {
  margin: 0 0 4px;
  font-size: 26px;
}
h2 {
  font-size: 16px;
  margin: 22px 0 10px;
  color: var(--primary);
}
.row {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 12px;
  margin-top: 18px;
}
</style>
