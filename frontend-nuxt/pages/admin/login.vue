<script setup lang="ts">
// 后台登录页(裸布局,未认证不显示导航)
definePageMeta({ layout: 'plain' })

const { login, getToken } = useAuth()
const { siteTitle, load } = useSettings()
load()

const identifier = ref('')
const password = ref('')
const err = ref('')
const busy = ref(false)

onMounted(() => {
  if (getToken()) location.replace('/admin')
})

const submit = async () => {
  err.value = ''
  busy.value = true
  try {
    await login(identifier.value.trim(), password.value)
    location.href = '/admin'
  } catch (e: any) {
    err.value = e?.message || '登录失败'
    busy.value = false
  }
}

useHead({ title: '登录 · fanke 后台', meta: [{ name: 'robots', content: 'noindex' }] })
</script>

<template>
  <main class="login-wrap">
    <el-form class="card login-card" label-position="top" autocomplete="off" @submit.prevent="submit">
      <h1>{{ siteTitle }}</h1>
      <p class="muted">管理后台登录</p>
      <el-form-item label="ID / 邮箱">
        <el-input v-model="identifier" required maxlength="120" autocomplete="username" placeholder="登录 ID 或邮箱地址" />
      </el-form-item>
      <el-form-item label="密码">
        <el-input v-model="password" type="password" show-password required autocomplete="current-password" />
      </el-form-item>
      <p v-if="err" class="err">{{ err }}</p>
      <el-button type="primary" native-type="submit" :disabled="busy">{{ busy ? '登录中…' : '登录' }}</el-button>
      <p class="muted back"><NuxtLink to="/">← 返回站点</NuxtLink></p>
    </el-form>
  </main>
</template>

<style scoped>
.login-wrap {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
}
.login-card {
  width: min(400px, 100%);
  padding: 30px 30px 24px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.login-card h1 {
  margin: 0;
  font-size: 24px;
  color: var(--text);
}
.login-card .muted {
  margin: -8px 0 4px;
}
.err {
  margin: 0;
  color: #d64545;
  font-size: 14px;
}
.back {
  text-align: center;
  margin: 4px 0 0;
}
</style>
