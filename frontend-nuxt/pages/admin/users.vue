<script setup lang="ts">
// 用户管理:新建/编辑资料/重置密码/删除(仅管理员)
definePageMeta({ layout: 'admin', middleware: 'auth' })
import type { AdminUser } from '~/types'

const { api } = useAuth()
const { push } = useToast()
const { run } = useBusy()

const list = ref<AdminUser[]>([])
const me = ref<AdminUser | null>(null)
const loading = ref(true)
const error = ref<any>(null)
// 当前正在编辑/新建的用户(null=新建,undefined=关闭)
const editing = ref<AdminUser | null | undefined>(undefined)
const formBox = ref<HTMLElement>()

const load = async () => {
  loading.value = true
  error.value = null
  editing.value = undefined
  try {
    ;[list.value, me.value] = await Promise.all([api<AdminUser[]>('/admin/users'), api<AdminUser>('/auth/me')])
  } catch (e: any) {
    error.value = e
  } finally {
    loading.value = false
  }
}
onMounted(load)

const openForm = (u: AdminUser | null) => {
  editing.value = u
  nextTick(() => formBox.value?.scrollIntoView({ behavior: 'smooth', block: 'start' }))
}

const resetPw = async (u: AdminUser) => {
  let np = ''
  try {
    const res = await ElMessageBox.prompt(`为 ${u.username} 设置新密码(至少 10 位)`, '重置密码', {
      inputType: 'password',
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      inputValidator: (v: string) => (v || '').length >= 10 || '密码至少 10 位',
    })
    np = res.value || ''
  } catch {
    return
  }
  if (np.length < 10) return push('密码至少 10 位', false)
  try {
    await run('重置中…', () => api(`/admin/users/${u.id}/password`, { method: 'POST', body: JSON.stringify({ password: np }) }))
    push('已重置', true)
  } catch (e: any) {
    push(e.message || '失败', false)
  }
}

const del = async (u: AdminUser) => {
  try {
    await ElMessageBox.confirm(`删除用户 ${u.username}?`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await run('删除中…', () => api(`/admin/users/${u.id}`, { method: 'DELETE' }))
    load()
  } catch (e: any) {
    push(e.message || '删除失败', false)
  }
}

// ---------- 批量删除(范式同 MediaLibrary;自己那一行不可勾选,后端也会逐项兜底) ----------
const tableRef = ref()
const selected = ref<Set<number>>(new Set())
const onSelChange = (rows: AdminUser[]) => {
  selected.value = new Set(rows.map((r) => r.id))
}
const selectable = (row: AdminUser) => row.id !== me.value?.id
const clearSel = () => {
  tableRef.value?.clearSelection()
  selected.value.clear()
}

const bulkDelete = async () => {
  if (!selected.value.size) return
  try {
    await ElMessageBox.confirm(`删除选中的 ${selected.value.size} 个用户?`, '批量删除', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    const ids = Array.from(selected.value)
    const res = await run('批量删除中…', () => api('/admin/users/bulk', { method: 'POST', body: JSON.stringify({ action: 'delete', ids }) }))
    push(`已删除 ${res.ok} 项`, true)
    if (res.failed?.length) (res.failed as string[]).forEach((f) => push(f, false))
    clearSel()
    load()
  } catch (e: any) {
    push(e.message || '批量删除失败', false)
  }
}

useHead({ title: '用户 · fanke 后台' })
</script>

<template>
  <div>
    <el-skeleton v-if="loading" :rows="6" animated />
    <ErrorBox v-else-if="error" :error="error" retry @retry="load" />
    <template v-else>
      <div class="panel card">
        <h2>站点用户</h2>
        <!-- 批量工具栏(全选本页用表头 selection 复选框;自己那一行不可勾选) -->
        <div v-if="selected.size" class="media-batch">
          <span class="muted">已选 {{ selected.size }} 项</span>
          <el-button size="small" type="danger" @click="bulkDelete">删除</el-button>
          <el-button size="small" text @click="clearSel">取消</el-button>
        </div>
        <el-table ref="tableRef" :data="list" stripe @selection-change="onSelChange">
          <el-table-column type="selection" width="42" :selectable="selectable" />
          <el-table-column label="ID" min-width="140">
            <template #default="{ row }">{{ row.username }}{{ row.id === me?.id ? '(我)' : '' }}</template>
          </el-table-column>
          <el-table-column prop="display_name" label="昵称" min-width="140" />
          <el-table-column prop="email" label="邮箱" min-width="200" />
          <el-table-column label="角色" width="110">
            <template #default="{ row }">
              <el-tag>{{ row.role === 'admin' ? '管理员' : '作者' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="注册时间" width="140">
            <template #default="{ row }"><span class="muted">{{ row.created_at.slice(0, 10) }}</span></template>
          </el-table-column>
          <el-table-column label="操作" width="220" fixed="right">
            <template #default="{ row }">
              <el-button size="small" @click="openForm(row)">编辑</el-button>
              <el-button size="small" @click="resetPw(row)">重置密码</el-button>
              <el-button v-if="row.id !== me?.id" size="small" type="danger" @click="del(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-button type="primary" style="margin-top: 12px" @click="openForm(null)">+ 新建用户</el-button>
      </div>
      <div v-if="editing !== undefined" ref="formBox">
        <UserForm :user="editing ?? null" @saved="load" @cancel="editing = undefined" />
      </div>
    </template>
  </div>
</template>
