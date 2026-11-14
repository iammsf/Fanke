<script setup lang="ts">
// 用户新建/编辑表单(用户管理页共用)
import type { AdminUser } from '~/types'

const props = defineProps<{ user: AdminUser | null }>()
const emit = defineEmits<{ saved: []; cancel: [] }>()

const { api } = useAuth()
const { push } = useToast()

const form = reactive({
  username: props.user?.username || '',
  email: props.user?.email || '',
  display_name: props.user?.display_name || '',
  role: props.user?.role || 'author',
  avatar_url: props.user?.avatar_url || '',
  bio: props.user?.bio || '',
  password: '',
})
const saving = ref(false)

const save = async () => {
  saving.value = true
  const body: any = {
    username: form.username.trim(),
    email: form.email.trim(),
    display_name: form.display_name.trim(),
    avatar_url: form.avatar_url.trim(),
    bio: form.bio,
    role: form.role,
  }
  try {
    if (props.user) {
      await api(`/admin/users/${props.user.id}`, { method: 'PUT', body: JSON.stringify(body) })
    } else {
      body.password = form.password
      await api('/admin/users', { method: 'POST', body: JSON.stringify(body) })
    }
    push('已保存', true)
    emit('saved')
  } catch (e: any) {
    push(e.message || '保存失败', false)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <el-form class="panel stack card" style="margin-top: 12px" label-position="top" autocomplete="off" @submit.prevent="save">
    <h2>{{ user ? `编辑:${user.username}` : '新建用户' }}</h2>
    <el-form-item label="登录 ID(唯一,用于登录)"><el-input v-model="form.username" name="username" placeholder="登录 ID(唯一)" autocomplete="username" /></el-form-item>
    <el-form-item label="邮箱"><el-input v-model="form.email" type="email" placeholder="邮箱地址" /></el-form-item>
    <el-form-item label="昵称(展示名称,不用于登录)"><el-input v-model="form.display_name" placeholder="昵称(展示名称,可重复)" /></el-form-item>
    <el-form-item label="角色">
      <el-select v-model="form.role">
        <el-option
          v-for="o in [{ value: 'author', label: '作者' }, { value: 'admin', label: '管理员' }]"
          :key="o.value"
          :label="o.label"
          :value="o.value"
          :disabled="o.disabled"
        />
      </el-select>
    </el-form-item>
    <el-form-item label="头像 URL(可选)"><el-input v-model="form.avatar_url" placeholder="头像 URL(可选)" /></el-form-item>
    <el-form-item label="简介"><el-input v-model="form.bio" type="textarea" :rows="3" placeholder="简介(可选)" /></el-form-item>
    <el-form-item v-if="!user" label="初始密码">
      <el-input v-model="form.password" type="password" show-password placeholder="初始密码(≥10 位,仅新建)" autocomplete="new-password" />
    </el-form-item>
    <div style="display: flex; gap: 10px">
      <el-button type="primary" native-type="submit" :disabled="saving">保存</el-button>
      <el-button @click="emit('cancel')">取消</el-button>
    </div>
  </el-form>
</template>
