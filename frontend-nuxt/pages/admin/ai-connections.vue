<script setup lang="ts">
definePageMeta({ layout: 'admin', middleware: 'auth' })
import { Plus, Delete } from '@element-plus/icons-vue'

interface AIConnection {
  id: number
  name: string
  group_name: string
  provider: 'openai' | 'claude'
  api_url: string
  api_key: string
  model: string
  enabled: boolean
  weight: number
}

interface ModelOption {
  model: string
  weight: number
  selected: boolean
}

const { api } = useAuth()
const { push } = useToast()
const { run } = useBusy()

const loading = ref(true)
const error = ref<any>(null)
const connections = ref<AIConnection[]>([])
// 还没有模型的空分组(后端保存在 settings.ai_groups)
const extraGroups = ref<string[]>([])

// 当前使用:group 决定分组;model 留空 = 组内按权重负载均衡
const selection = reactive({ group: '', model: '' })

const connGroups = computed(() => {
  const map = new Map<string, AIConnection[]>()
  for (const c of connections.value) {
    const g = c.group_name || '默认'
    if (!map.has(g)) map.set(g, [])
    map.get(g)!.push(c)
  }
  return map
})

const groupNames = computed(() => {
  const names = Array.from(connGroups.value.keys())
  for (const n of extraGroups.value) {
    if (n && !connGroups.value.has(n)) names.push(n)
  }
  return names
})

const groups = computed(() => groupNames.value.map(name => ({ name, items: connGroups.value.get(name) || [] })))

const load = async () => {
  loading.value = true
  error.value = null
  try {
    connections.value = (await api<AIConnection[]>('/admin/ai-connections')) || []
    const settings = await api<Record<string, any>>('/admin/settings')
    const saved = settings?.ai_selection
    const sel = typeof saved === 'string' ? JSON.parse(saved) : saved
    const savedGroups = settings?.ai_groups
    const extra = typeof savedGroups === 'string' ? JSON.parse(savedGroups) : savedGroups
    extraGroups.value = Array.isArray(extra) ? extra.filter((x: any) => typeof x === 'string') : []
    selection.group = sel?.group || groupNames.value[0] || ''
    selection.model = sel?.model || ''
  } catch (e: any) {
    error.value = e
  } finally {
    loading.value = false
  }
}

onMounted(load)

const saveSelection = async () => {
  const group = selection.group.trim()
  if (!group) {
    push('请先选择分组', false)
    return
  }
  try {
    await run('保存中…', () => api(`/admin/settings?key=ai_selection`, {
      method: 'PUT',
      body: JSON.stringify({ group, model: selection.model.trim() }),
    }))
    push(selection.model ? `已指定使用「${group} / ${selection.model}」` : `已设为分组「${group}」按权重负载均衡`, true)
  } catch (e: any) {
    push(e.message || '保存失败', false)
  }
}

// ---------- 批量添加(同一分组、同一 API 凭据,多模型) ----------
const batchVisible = ref(false)
const loadingModels = ref(false)
const batchForm = reactive({
  group: '',
  provider: 'openai' as 'openai' | 'claude',
  api_url: '',
  api_key: '',
  enabled: true,
})
// 分组已有连接时可直接复用已保存的 API 地址与密钥(后端返回的本来就是脱敏值)
const useSavedCred = ref(false)
const modelOptions = ref<ModelOption[]>([])
const modelKeyword = ref('')

const savedCred = computed(() => {
  const g = batchForm.group.trim()
  return connections.value.find(c => c.group_name === g) || null
})

watch(() => batchForm.group, () => {
  if (savedCred.value) useSavedCred.value = true
})

const visibleModels = computed(() => {
  const kw = modelKeyword.value.trim().toLowerCase()
  return kw ? modelOptions.value.filter(m => m.model.toLowerCase().includes(kw)) : modelOptions.value
})
const selectedCount = computed(() => modelOptions.value.filter(m => m.selected).length)
const existingModels = computed(() => new Set(
  connections.value.filter(c => c.group_name === batchForm.group.trim()).map(c => c.model),
))

const openBatch = (group?: string) => {
  Object.assign(batchForm, {
    group: group || selection.group || groupNames.value[0] || '',
    provider: 'openai',
    api_url: '',
    api_key: '',
    enabled: true,
  })
  useSavedCred.value = !!savedCred.value
  modelOptions.value = []
  modelKeyword.value = ''
  batchVisible.value = true
}

const fetchModels = async () => {
  const reuse = useSavedCred.value && !!savedCred.value
  if (!reuse && (!batchForm.api_url.trim() || !batchForm.api_key.trim())) {
    push('请先填写 API 地址和 API Key', false)
    return
  }
  loadingModels.value = true
  try {
    const result = await run('查询模型中…', () =>
      api<{ models?: string[] }>('/admin/ai-models', {
        method: 'POST',
        body: JSON.stringify(reuse
          ? { group: batchForm.group.trim() }
          : { provider: batchForm.provider, api_url: batchForm.api_url.trim(), api_key: batchForm.api_key.trim() }),
      }),
    )
    const list = Array.isArray(result?.models) ? result.models : []
    const exist = existingModels.value
    modelOptions.value = list.map(model => ({ model, weight: 1, selected: !exist.has(model) }))
    push(list.length ? `已找到 ${list.length} 个模型` : '未找到可用模型', !!list.length)
  } catch (e: any) {
    push(e.message || '查询模型失败', false)
  } finally {
    loadingModels.value = false
  }
}

const toggleAll = (checked: boolean) => {
  for (const m of modelOptions.value) {
    if (!existingModels.value.has(m.model)) m.selected = checked
  }
}

const saveBatch = async () => {
  const group = batchForm.group.trim()
  const picked = modelOptions.value.filter(m => m.selected)
  const reuse = useSavedCred.value && !!savedCred.value
  if (!group) {
    push('请填写分组名称', false)
    return
  }
  if (!reuse && (!batchForm.api_url.trim() || !batchForm.api_key.trim())) {
    push('请填写 API 地址和 API Key', false)
    return
  }
  if (!picked.length) {
    push('请至少勾选一个模型', false)
    return
  }
  try {
    await run('添加模型中…', () => api('/admin/ai-connections/batch', {
      method: 'POST',
      body: JSON.stringify({
        group,
        enabled: batchForm.enabled,
        models: picked.map(m => ({ model: m.model, weight: m.weight || 1 })),
        ...(reuse ? {} : {
          provider: batchForm.provider,
          api_url: batchForm.api_url.trim(),
          api_key: batchForm.api_key.trim(),
        }),
      }),
    }))
    push(`已向分组「${group}」添加 ${picked.length} 个模型`, true)
    if (!selection.group) selection.group = group
    batchVisible.value = false
    await load()
  } catch (e: any) {
    push(e.message || '添加失败', false)
  }
}

// ---------- 单条编辑 ----------
const editVisible = ref(false)
const editingId = ref<number | null>(null)
// 后端只返回脱敏密钥;编辑时留空表示不修改
const maskedKeyHint = ref('')
const editForm = reactive({
  name: '',
  group_name: '',
  provider: 'openai' as 'openai' | 'claude',
  api_url: '',
  api_key: '',
  model: '',
  weight: 1,
  enabled: true,
})

const openEdit = (item: AIConnection) => {
  Object.assign(editForm, {
    name: item.name,
    group_name: item.group_name,
    provider: item.provider,
    api_url: item.api_url,
    api_key: '',
    model: item.model,
    weight: item.weight,
    enabled: item.enabled,
  })
  maskedKeyHint.value = item.api_key
  editingId.value = item.id
  editVisible.value = true
}

const saveEdit = async () => {
  if (!editingId.value) return
  if (!editForm.name.trim() || !editForm.group_name.trim() || !editForm.model.trim()) {
    push('名称、分组和模型不能为空', false)
    return
  }
  try {
    await run('保存中…', () => api(`/admin/ai-connections/${editingId.value}`, {
      method: 'PUT',
      body: JSON.stringify({
        ...editForm,
        name: editForm.name.trim(),
        group_name: editForm.group_name.trim(),
        model: editForm.model.trim(),
        api_url: editForm.api_url.trim(),
        api_key: editForm.api_key.trim() || undefined,
        weight: Math.max(1, editForm.weight || 1),
      }),
    }))
    push('连接已更新', true)
    editVisible.value = false
    await load()
  } catch (e: any) {
    push(e.message || '保存失败', false)
  }
}

// ---------- 分组管理:新建 / 重命名 / 删除 ----------
const groupVisible = ref(false)
const newGroupName = ref('')

const openCreateGroup = () => {
  newGroupName.value = ''
  groupVisible.value = true
}

const createGroup = async () => {
  const name = newGroupName.value.trim()
  if (!name) {
    push('请填写分组名称', false)
    return
  }
  try {
    await run('创建中…', () => api('/admin/ai-connections/group-create', {
      method: 'POST',
      body: JSON.stringify({ name }),
    }))
    push(`分组「${name}」已创建`, true)
    groupVisible.value = false
    if (!selection.group) selection.group = name
    await load()
  } catch (e: any) {
    push(e.message || '创建失败', false)
  }
}

const renameVisible = ref(false)
const renameFrom = ref('')
const renameTo = ref('')

const openRename = (name: string) => {
  renameFrom.value = name
  renameTo.value = name
  renameVisible.value = true
}

const confirmRename = async () => {
  const next = renameTo.value.trim()
  if (!next || next === renameFrom.value) {
    renameVisible.value = false
    return
  }
  try {
    const res = await run('重命名中…', () => api<{ updated: number }>('/admin/ai-connections/group-rename', {
      method: 'POST',
      body: JSON.stringify({ from: renameFrom.value, to: next }),
    }))
    push(`已重命名 ${res?.updated ?? 0} 条连接`, true)
    if (selection.group === renameFrom.value) selection.group = next
    renameVisible.value = false
    await load()
  } catch (e: any) {
    push(e.message || '重命名失败', false)
  }
}

const delVisible = ref(false)
const delForm = reactive({
  name: '',
  action: 'delete' as 'delete' | 'move',
  target: '',
})
const delModelCount = computed(() => (delForm.name ? (connGroups.value.get(delForm.name)?.length ?? 0) : 0))
const delOtherGroups = computed(() => groupNames.value.filter(n => n !== delForm.name))

const openDeleteGroup = (name: string) => {
  delForm.name = name
  delForm.action = 'delete'
  delForm.target = groupNames.value.find(n => n !== name) || ''
  delVisible.value = true
}

const confirmDeleteGroup = async () => {
  if (delForm.action === 'move' && !delForm.target) {
    push('请选择迁移目标分组', false)
    return
  }
  try {
    const res = await run('删除分组中…', () => api<{ affected: number }>('/admin/ai-connections/group-delete', {
      method: 'POST',
      body: JSON.stringify({
        name: delForm.name,
        action: delForm.action,
        target: delForm.action === 'move' ? delForm.target : '',
      }),
    }))
    push(
      delForm.action === 'move'
        ? `已将 ${res?.affected ?? 0} 个模型迁移到「${delForm.target}」`
        : `已删除分组「${delForm.name}」及其 ${res?.affected ?? 0} 个模型`,
      true,
    )
    delVisible.value = false
    await load()
  } catch (e: any) {
    push(e.message || '删除失败', false)
  }
}

const updateWeight = async (item: AIConnection, weight: number) => {
  const w = Math.max(1, Math.floor(weight || 1))
  if (w === item.weight) return
  try {
    await api(`/admin/ai-connections/${item.id}`, { method: 'PUT', body: JSON.stringify({ weight: w }) })
    item.weight = w
    push('权重已更新', true)
  } catch (e: any) {
    push(e.message || '权重更新失败', false)
  }
}

const onWeightChange = (item: AIConnection, value: number | undefined) => {
  void updateWeight(item, value || 1)
}

const remove = async (item: AIConnection) => {
  try {
    await ElMessageBox.confirm(`确定删除「${item.group_name} / ${item.model}」吗？`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await run('删除中…', () => api(`/admin/ai-connections/${item.id}`, { method: 'DELETE' }))
    push('连接已删除', true)
    await load()
  } catch (e: any) {
    push(e.message || '删除失败', false)
  }
}

const toggle = async (item: AIConnection) => {
  const enabled = !item.enabled
  try {
    await api(`/admin/ai-connections/${item.id}`, { method: 'PUT', body: JSON.stringify({ enabled }) })
    item.enabled = enabled
  } catch (e: any) {
    push(e.message || '状态更新失败', false)
  }
}

useHead({ title: 'AI 连接 · fanke 后台' })
</script>

<template>
  <div>
    <section class="panel card">
      <div class="toolbar">
        <div>
          <h2 style="margin: 0">AI 连接</h2>
          <p class="muted" style="margin-bottom: 0">
            按分组管理模型：同组按权重负载均衡，也可指定组内某个模型。
          </p>
        </div>
        <div class="toolbar-actions">
          <el-button @click="openCreateGroup">
            <el-icon><Plus /></el-icon>新建分组
          </el-button>
          <el-button type="primary" @click="openBatch()">批量添加模型</el-button>
        </div>
      </div>
    </section>

    <section class="panel card">
      <h3 style="margin: 0 0 12px">当前使用</h3>
      <div class="selection-row">
        <el-select v-model="selection.group" placeholder="选择分组" style="min-width: 180px" filterable>
          <el-option v-for="name in groupNames" :key="name" :label="name" :value="name" />
        </el-select>
        <el-select v-model="selection.model" placeholder="留空 = 组内按权重负载均衡" style="min-width: 240px" clearable filterable>
          <el-option
            v-for="c in connections.filter(c => c.group_name === selection.group && c.enabled)"
            :key="c.id"
            :label="c.model"
            :value="c.model"
          />
        </el-select>
        <el-button type="primary" @click="saveSelection">保存选择</el-button>
      </div>
      <p class="muted" style="margin: 10px 0 0">
        插件调用 <code>ai.chat</code> 未指定分组/模型时，会套用这里的设置。
      </p>
    </section>

    <el-skeleton v-if="loading" :rows="6" animated />
    <ErrorBox v-else-if="error" :error="error" retry @retry="load" />
    <template v-else>
      <el-empty v-if="!connections.length" description="暂无 AI 连接，点击上方按钮批量添加" />
      <section v-for="g in groups" :key="g.name" class="panel card">
        <div class="group-head">
          <div>
            <h3 style="margin: 0; display: flex; align-items: center; gap: 8px">
              {{ g.name }}
              <el-tag v-if="selection.group === g.name" size="small" type="success">当前使用</el-tag>
            </h3>
            <p class="muted" style="margin: 4px 0 0">{{ g.items.length }} 个模型</p>
            <p v-if="g.items.length" class="muted cred-hint">API {{ g.items[0].api_url }} · Key {{ g.items[0].api_key }}</p>
          </div>
          <div class="toolbar-actions">
            <el-button size="small" @click="selection.group = g.name; selection.model = ''">设为当前分组</el-button>
            <el-button size="small" @click="openBatch(g.name)">添加模型</el-button>
            <el-button size="small" @click="openRename(g.name)">重命名</el-button>
            <el-button size="small" type="danger" plain @click="openDeleteGroup(g.name)">
              <el-icon><Delete /></el-icon>删除分组
            </el-button>
          </div>
        </div>

        <el-empty v-if="!g.items.length" description="该分组暂无模型，点击「添加模型」批量导入" :image-size="70" />
        <el-table v-else :data="g.items" stripe>
          <el-table-column label="模型" min-width="200">
            <template #default="{ row }">
              <span>{{ row.model }}</span>
              <el-tag v-if="selection.group === g.name && selection.model === row.model" size="small" style="margin-left: 8px">指定中</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="Provider" width="110">
            <template #default="{ row }">
              <el-tag :type="row.provider === 'openai' ? 'success' : 'warning'">{{ row.provider === 'openai' ? 'OpenAI' : 'Claude' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="权重" width="130">
            <template #default="{ row }">
              <el-input-number
                :model-value="row.weight"
                :min="1"
                :precision="0"
                size="small"
                controls-position="right"
                style="width: 100px"
                @change="(v) => onWeightChange(row, v)"
              />
            </template>
          </el-table-column>
          <el-table-column label="启用" width="90">
            <template #default="{ row }"><el-switch v-model="row.enabled" @change="toggle(row)" /></template>
          </el-table-column>
          <el-table-column label="操作" width="150" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
              <el-button link type="danger" @click="remove(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </section>
    </template>

    <!-- 批量添加 -->
    <el-dialog v-model="batchVisible" title="批量添加模型" width="min(720px, calc(100vw - 32px))" destroy-on-close>
      <el-form label-position="top" @submit.prevent="saveBatch">
        <el-form-item label="分组名称" required>
          <el-select v-model="batchForm.group" filterable allow-create default-first-option placeholder="例如：主力模型 / 便宜备用" style="width: 100%">
            <el-option v-for="name in groupNames" :key="name" :label="name" :value="name" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="savedCred" label="分组已保存的 API 凭据">
          <div class="cred-box">
            <div class="cred-row"><span class="cred-k">API</span><span class="mono">{{ savedCred.api_url }}</span></div>
            <div class="cred-row"><span class="cred-k">Key</span><span class="mono">{{ savedCred.api_key }}</span></div>
            <div class="cred-row"><span class="cred-k">Provider</span><span>{{ savedCred.provider }}</span></div>
            <el-switch
              v-model="useSavedCred"
              size="small"
              active-text="复用已保存凭据"
              inactive-text="重新填写凭据"
              style="margin-top: 8px"
            />
            <p class="muted cred-note">密钥仅以脱敏形式展示，明文不会返回浏览器。</p>
          </div>
        </el-form-item>
        <template v-if="!savedCred || !useSavedCred">
          <el-form-item label="Provider" required>
            <el-select v-model="batchForm.provider" style="width: 100%">
              <el-option label="OpenAI" value="openai" />
              <el-option label="Claude" value="claude" />
            </el-select>
          </el-form-item>
          <el-form-item label="API 地址" required>
            <el-input v-model="batchForm.api_url" placeholder="https://api.openai.com/v1" />
          </el-form-item>
          <el-form-item label="API Key" required>
            <el-input v-model="batchForm.api_key" type="password" show-password autocomplete="new-password" />
          </el-form-item>
        </template>
        <div class="fetch-row">
          <el-button :loading="loadingModels" @click="fetchModels">获取模型列表</el-button>
          <template v-if="modelOptions.length">
            <el-input v-model="modelKeyword" placeholder="筛选模型" clearable style="width: 200px" />
            <el-button size="small" @click="toggleAll(true)">全选</el-button>
            <el-button size="small" @click="toggleAll(false)">清空</el-button>
            <span class="muted">已选 {{ selectedCount }} / {{ modelOptions.length }}</span>
          </template>
        </div>
        <div v-if="modelOptions.length" class="model-picker">
          <label v-for="m in visibleModels" :key="m.model" class="model-item" :class="{ disabled: existingModels.has(m.model) }">
            <el-checkbox v-model="m.selected" :disabled="existingModels.has(m.model)" />
            <span class="model-name">{{ m.model }}</span>
            <el-tag v-if="existingModels.has(m.model)" size="small" type="info">已添加</el-tag>
            <el-input-number v-else v-model="m.weight" :min="1" :precision="0" size="small" controls-position="right" style="width: 96px" />
          </label>
        </div>
        <el-form-item label="状态" style="margin-top: 12px">
          <el-switch v-model="batchForm.enabled" active-text="启用" inactive-text="停用" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="batchVisible = false">取消</el-button>
        <el-button type="primary" :disabled="!selectedCount" @click="saveBatch">
          添加 {{ selectedCount }} 个模型
        </el-button>
      </template>
    </el-dialog>

    <!-- 单条编辑 -->
    <el-dialog v-model="editVisible" title="编辑 AI 连接" width="min(620px, calc(100vw - 32px))" destroy-on-close>
      <el-form label-position="top" @submit.prevent="saveEdit">
        <div class="form-cols">
          <el-form-item label="名称" required><el-input v-model="editForm.name" /></el-form-item>
          <el-form-item label="分组" required>
            <el-select v-model="editForm.group_name" filterable allow-create default-first-option style="width: 100%">
              <el-option v-for="name in groupNames" :key="name" :label="name" :value="name" />
            </el-select>
          </el-form-item>
        </div>
        <div class="form-cols">
          <el-form-item label="Provider" required>
            <el-select v-model="editForm.provider" style="width: 100%">
              <el-option label="OpenAI" value="openai" />
              <el-option label="Claude" value="claude" />
            </el-select>
          </el-form-item>
          <el-form-item label="模型" required><el-input v-model="editForm.model" /></el-form-item>
        </div>
        <el-form-item label="API 地址" required><el-input v-model="editForm.api_url" /></el-form-item>
        <el-form-item label="API Key">
          <el-input
            v-model="editForm.api_key"
            type="password"
            show-password
            autocomplete="new-password"
            :placeholder="maskedKeyHint ? `已保存：${maskedKeyHint}（留空不修改）` : '留空则不修改'"
          />
        </el-form-item>
        <div class="form-cols">
          <el-form-item label="权重（组内负载均衡）" required>
            <el-input-number v-model="editForm.weight" :min="1" :precision="0" controls-position="right" style="width: 100%" />
          </el-form-item>
          <el-form-item label="状态">
            <el-switch v-model="editForm.enabled" active-text="启用" inactive-text="停用" />
          </el-form-item>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="editVisible = false">取消</el-button>
        <el-button type="primary" @click="saveEdit">保存</el-button>
      </template>
    </el-dialog>

    <!-- 新建分组 -->
    <el-dialog v-model="groupVisible" title="新建分组" width="min(440px, calc(100vw - 32px))" destroy-on-close>
      <el-form label-position="top" @submit.prevent="createGroup">
        <el-form-item label="分组名称" required>
          <el-input v-model="newGroupName" maxlength="64" show-word-limit placeholder="例如：主力模型 / 便宜备用" />
        </el-form-item>
      </el-form>
      <p class="muted" style="margin: 0">分组创建后为空，可在分组卡片上点「添加模型」批量导入。</p>
      <template #footer>
        <el-button @click="groupVisible = false">取消</el-button>
        <el-button type="primary" :disabled="!newGroupName.trim()" @click="createGroup">创建</el-button>
      </template>
    </el-dialog>

    <!-- 重命名分组 -->
    <el-dialog v-model="renameVisible" title="重命名分组" width="min(440px, calc(100vw - 32px))" destroy-on-close>
      <el-form label-position="top" @submit.prevent="confirmRename">
        <el-form-item label="新名称" required>
          <el-input v-model="renameTo" maxlength="64" show-word-limit />
        </el-form-item>
      </el-form>
      <p class="muted" style="margin: 0">组内模型与「当前使用」设置会同步改到新名称。</p>
      <template #footer>
        <el-button @click="renameVisible = false">取消</el-button>
        <el-button type="primary" :disabled="!renameTo.trim()" @click="confirmRename">保存</el-button>
      </template>
    </el-dialog>

    <!-- 删除分组:决定组内模型的去留 -->
    <el-dialog v-model="delVisible" title="删除分组" width="min(520px, calc(100vw - 32px))" destroy-on-close>
      <el-alert
        :title="`分组「${delForm.name}」下有 ${delModelCount} 个模型，请选择这些模型的处理方式`"
        type="warning"
        :closable="false"
        show-icon
        style="margin-bottom: 16px"
      />
      <el-radio-group v-model="delForm.action" class="del-options">
        <el-radio value="delete">连同组内 {{ delModelCount }} 个模型一起删除（不可恢复）</el-radio>
        <el-radio value="move">把 {{ delModelCount }} 个模型迁移到其它分组</el-radio>
      </el-radio-group>
      <div v-if="delForm.action === 'move'" style="margin-top: 14px">
        <el-select v-model="delForm.target" placeholder="选择目标分组" filterable style="width: 100%">
          <el-option v-for="n in delOtherGroups" :key="n" :label="n" :value="n" />
        </el-select>
        <p v-if="!delOtherGroups.length" class="muted" style="margin: 8px 0 0">
          还没有其它分组，请先新建一个再迁移。
        </p>
        <p v-else class="muted" style="margin: 8px 0 0">迁移只改归属分组，模型、密钥与权重都保留。</p>
      </div>
      <template #footer>
        <el-button @click="delVisible = false">取消</el-button>
        <el-button
          :type="delForm.action === 'delete' ? 'danger' : 'primary'"
          :disabled="delForm.action === 'move' && !delForm.target"
          @click="confirmDeleteGroup"
        >
          {{ delForm.action === 'delete' ? '删除分组与模型' : '迁移并删除分组' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.toolbar { display: flex; align-items: center; justify-content: space-between; gap: 16px; flex-wrap: wrap; }
.toolbar-actions { display: inline-flex; gap: 8px; flex-wrap: wrap; }
.group-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; margin-bottom: 12px; flex-wrap: wrap; }
.selection-row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
.fetch-row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; margin-bottom: 10px; }
.model-picker { max-height: 300px; overflow: auto; border: 1px solid var(--el-border-color, #dcdfe6); border-radius: 8px; padding: 8px; }
.model-item { display: flex; align-items: center; gap: 10px; padding: 6px 4px; }
.model-item.disabled { opacity: .6; }
.model-name { flex: 1; word-break: break-all; }
.form-cols { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.cred-box { width: 100%; padding: 10px 12px; border: 1px solid var(--el-border-color, #dcdfe6); border-radius: 8px; background: var(--surface-2); }
.cred-row { display: flex; gap: 8px; font-size: 13px; line-height: 1.8; word-break: break-all; }
.cred-k { flex: none; width: 62px; color: var(--text); }
.cred-note { margin: 6px 0 0; font-size: 12px; }
.cred-hint { margin: 2px 0 0; font-size: 12.5px; word-break: break-all; }
.del-options { display: flex; flex-direction: column; align-items: flex-start; gap: 14px; }
@media (max-width: 600px) { .form-cols { display: block; } }
</style>
