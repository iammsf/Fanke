<script setup lang="ts">
// 媒体库网格:manage=管理(多选/删除/转移/下载),pick=选择器(选为封面)。
// image-focus 插件启用时:缩略图应用焦点数据,图片卡多出"选择焦点"按钮。
import type { MediaItem, Page } from '~/types'

const props = withDefaults(defineProps<{ mode?: 'manage' | 'pick'; pageSize?: number }>(), { mode: 'manage', pageSize: 24 })
const emit = defineEmits<{ pick: [media: MediaItem]; deleted: [] }>()

const { api, getToken, apiBase } = useAuth()
const { push } = useToast()
const { run } = useBusy() // 与 DB/存储交互期间的全屏加载遮罩(兼防连点)
const { isEnabled, refreshPublic, loaded, contributions, contributionsLoaded, refreshContributions } = useExtensions()

const focusActive = computed(() => isEnabled('image-focus'))
const s3Storage = computed(() => contributions.value?.s3_storage === true)
if (import.meta.client && !contributionsLoaded.value) void refreshContributions()
if (import.meta.client && !loaded.value) void refreshPublic()

// pick 模式:根据网格容器可用宽高动态计算列数/行数,每页数量=列×行
// 用根 div 作为观测目标(始终渲染,不像 media-grid 那样在 loading 时被 v-if 移除)
const rootRef = ref<HTMLElement>()
// 初始值用窗口尺寸粗算,确保首次 load 能拿到 >0 的 PER 而不会被 PER===0 挡住
const initCols = Math.max(1, Math.floor((typeof window !== 'undefined' ? window.innerWidth * 0.7 : 1200) / 184))
const initRows = Math.max(1, Math.floor((typeof window !== 'undefined' ? window.innerHeight * 0.7 : 700) / 204))
const cols = ref(props.mode === 'pick' ? 0 : initCols)
const rows = ref(props.mode === 'pick' ? 0 : initRows)
const cardW = ref(170)
const cardH = ref(190)
const updateGrid = () => {
  const el = rootRef.value
  if (!el) return
  const rect = el.getBoundingClientRect()
  if (rect.width < 20 || rect.height < 20) return // 对话框未打开/无尺寸时跳过
  const gap = 14
  const cardMinW = 170
  const cardMinH = 190
  const paginationH = 52 // 底部分页器高度,需从可用高度中扣除
  const gridH = Math.max(20, rect.height - paginationH)
  // 向下取整算出最多能放几列几行
  cols.value = Math.max(1, Math.floor((rect.width + gap) / (cardMinW + gap)))
  rows.value = Math.max(1, Math.floor((gridH + gap) / (cardMinH + gap)))
  // 实际卡片尺寸:按列数/行数平分可用空间
  cardW.value = Math.max(cardMinW, Math.floor((rect.width - (cols.value - 1) * gap) / cols.value))
  cardH.value = Math.max(cardMinH, Math.floor((gridH - (rows.value - 1) * gap) / rows.value))
}
let ro: ResizeObserver | null = null
let gridTimer: ReturnType<typeof setTimeout> | null = null
// 对话框打开动画期间尺寸每帧都在变,防抖到稳定后再算一次,避免反复改变 PER 触发连续请求。
const scheduleGrid = () => {
  if (gridTimer) clearTimeout(gridTimer)
  gridTimer = setTimeout(() => {
    gridTimer = null
    updateGrid()
  }, 180)
}
onMounted(() => {
  if (import.meta.client) {
    ro = new ResizeObserver(scheduleGrid)
    if (rootRef.value) ro.observe(rootRef.value)
    scheduleGrid()
  }
})
onBeforeUnmount(() => {
  ro?.disconnect()
  if (gridTimer) clearTimeout(gridTimer)
})

const PER = computed(() => (props.mode === 'pick' ? cols.value * rows.value : props.pageSize))
const page = ref(1)
const data = ref<Page<MediaItem> | null>(null)
const loading = ref(true)
const error = ref<any>(null)
const selected = ref<Set<number>>(new Set())

const load = async () => {
  // pick 模式下,网格尚无有效尺寸(cols/rows 为 0)时不请求,等对话框打开后 ResizeObserver 算出尺寸再加载
  if (props.mode === 'pick' && PER.value === 0) return
  loading.value = true
  error.value = null
  try {
    data.value = await api<Page<MediaItem>>(`/admin/media?per=${PER.value}&page=${page.value}`)
  } catch (e: any) {
    error.value = e
  } finally {
    loading.value = false
  }
}
onMounted(load)
watch(PER, () => {
  page.value = 1
  selected.value.clear()
  load()
})
defineExpose({ reload: load })

const toggle = (id: number) => {
  if (selected.value.has(id)) selected.value.delete(id)
  else selected.value.add(id)
}
const toggleAll = () => {
  const ids = data.value?.items.map((i) => i.id) ?? []
  const all = ids.every((id) => selected.value.has(id))
  ids.forEach((id) => {
    if (all) selected.value.delete(id)
    else selected.value.add(id)
  })
}

const del = async (m: MediaItem) => {
  try {
    await ElMessageBox.confirm(`删除 ${m.filename}(#${m.id})?`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await run('删除中…', () => api(`/admin/media/${m.id}`, { method: 'DELETE' }))
    push('已删除', true)
    emit('deleted')
    load()
  } catch (e: any) {
    push(e.message || '删除失败', false)
  }
}

const batchDelete = async () => {
  if (!selected.value.size) return
  try {
    await ElMessageBox.confirm(`删除选中的 ${selected.value.size} 项?`, '批量删除', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    const ids = Array.from(selected.value)
    const res = await run('批量删除中…', () => api('/admin/media/batch-delete', { method: 'POST', body: JSON.stringify({ ids }) }))
    push(`已删除 ${res.ok} 项`, true)
    if (res.failed?.length) (res.failed as string[]).forEach((f) => push(f, false))
    selected.value.clear()
    load()
  } catch (e: any) {
    push(e.message || '批量删除失败', false)
  }
}

const batchTransfer = async () => {
  if (!selected.value.size) return
  try {
    await ElMessageBox.confirm(`将 ${selected.value.size} 项转移到 S3?`, '转移到 S3', {
      type: 'warning',
      confirmButtonText: '转移',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    const ids = Array.from(selected.value)
    const res = await run('转移到 S3 中…', () => api('/admin/media/batch-transfer-s3', { method: 'POST', body: JSON.stringify({ ids }) }))
    push(`已转移 ${res.ok} 项, 跳过 ${res.skipped} 项, 更新 ${res.rewritten ?? 0} 处文章引用 / ${res.ext_rewritten ?? 0} 条插件记录`, true)
    if (res.failed?.length) (res.failed as string[]).forEach((f) => push(f, false))
    selected.value.clear()
    load()
  } catch (e: any) {
    push(e.message || '转移失败', false)
  }
}

const batchDownload = async () => {
  if (!selected.value.size) return
  const t = getToken()
  if (!t) { push('未登录', false); return }
  try {
    const ids = Array.from(selected.value)
    const blob = await run('打包下载中…', async () => {
      const res = await fetch(`${apiBase}/admin/media/batch-download`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${t}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({ ids }),
      })
      if (!res.ok) throw new Error(`下载失败(${res.status})`)
      return res.blob()
    })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `media-${Date.now()}.zip`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  } catch (e: any) {
    push(e.message || '下载失败', false)
  }
}

// 焦点编辑器
const focusTarget = ref<MediaItem | null>(null)
const focusOpen = ref(false)
const openFocus = (m: MediaItem) => {
  focusTarget.value = m
  focusOpen.value = true
}

// 图片预览(Element Plus 图片查看器):点缩略图/预览按钮,可在本页图片间左右切换
const viewerOpen = ref(false)
const viewerIndex = ref(0)
const imageItems = computed(() => (data.value?.items ?? []).filter((m) => m.mime.startsWith('image/')))
const viewerUrls = computed(() => imageItems.value.map((m) => m.url))
const openViewer = (m: MediaItem) => {
  if (!imageItems.value.length) return
  viewerIndex.value = Math.max(0, imageItems.value.findIndex((i) => i.id === m.id))
  viewerOpen.value = true
}

// ---------- 媒体库同步:修复「S3 桶对象 ↔ media 表记录」不一致 ----------
// 桶内孤儿 = 桶里有对象但媒体库没记录(多为直接从 S3 面板/其他工具上传,或上传中断的碎片);
// 悬空记录 = 媒体库有 r2 记录但桶里对象没了(多为误删对象)。
// 扫描纯只读;任何写操作都必须先勾选、再二次确认。
interface SyncObject {
  key: string
  filename: string
  size: number
  mime: string
  url: string
  allowed: boolean // 是否符合当前桶内容策略(allowed)
  ref_posts: number // 被文章正文直接引用的篇数(删桶对象前必须看,防误删在用的文件)
  last_modified: string
}
interface SyncRecord {
  id: number
  filename: string
  key: string
  url: string
  mime: string
  size: number
  created_at: string
  ref_posts: number // 引用该媒体的文章数
}
interface SyncResult {
  bucket: string
  public_base: string
  allowed: string
  totals: {
    bucket_objects: number
    bucket_truncated: boolean
    records: number
    s3_records: number
    local_records: number
    orphans: number
    missing: number
    other_source: number
  }
  orphans: SyncObject[]
  missing: SyncRecord[]
  other_source: SyncRecord[] // 指向历史桶/别的域名的记录,不参与对账,只读展示
}

const syncOpen = ref(false)
const syncing = ref(false)
const applying = ref(false)
const scan = ref<SyncResult | null>(null)
const syncTab = ref('orphan')
const orphanPick = ref<SyncObject[]>([])
const missPick = ref<SyncRecord[]>([])

const openSync = () => {
  syncOpen.value = true
  if (!scan.value) void doScan()
}

const doScan = async () => {
  syncing.value = true
  try {
    scan.value = await run('扫描 S3 桶中…', () =>
      api<SyncResult>('/admin/media/sync/scan', { method: 'POST', body: '{}' }),
    )
    orphanPick.value = []
    missPick.value = []
  } catch (e: any) {
    push(e.message || '扫描失败', false)
  } finally {
    syncing.value = false
  }
}

const applySync = async (
  payload: { import_keys?: string[]; delete_record_ids?: number[]; delete_object_keys?: string[] },
  label: string,
) => {
  applying.value = true
  try {
    const res: any = await run(`${label}中…`, () =>
      api('/admin/media/sync/apply', { method: 'POST', body: JSON.stringify(payload) }),
    )
    const parts = [`登记入库 ${res.imported} 项`]
    if (res.skipped_duplicate) parts.push(`跳过重复 ${res.skipped_duplicate} 项`)
    if (res.records_deleted) parts.push(`删记录 ${res.records_deleted} 条`)
    if (res.objects_deleted) parts.push(`删对象 ${res.objects_deleted} 个`)
    push(`${label}完成:${parts.join('、')}`, true)
    if (res.failed?.length) (res.failed as string[]).forEach((f: string) => push(f, false))
    if (res.imported || res.records_deleted) load() // 入库/删记录都会改变媒体库列表,顺手刷新
    await doScan() // 重新对账,让差异清单回到最新
  } catch (e: any) {
    push(e.message || `${label}失败`, false)
  } finally {
    applying.value = false
  }
}

const importOrphans = async () => {
  const keys = orphanPick.value.map((o) => o.key)
  if (!keys.length) return
  const bad = orphanPick.value.filter((o) => !o.allowed).length
  const extra = bad
    ? `\n注意:其中 ${bad} 个不符合当前内容策略(allowed=${scan.value?.allowed ?? ''}),登记后数据库有记录但前台可能不允许直接访问。`
    : ''
  try {
    await ElMessageBox.confirm(`把选中的 ${keys.length} 个桶内对象登记进媒体库?${extra}`, '登记入库', {
      type: 'warning',
      confirmButtonText: '登记',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  applySync({ import_keys: keys }, '登记入库')
}

const dropOrphans = async () => {
  const keys = orphanPick.value.map((o) => o.key)
  if (!keys.length) return
  const used = orphanPick.value.filter((o) => o.ref_posts > 0)
  const preview = keys.slice(0, 5).join('\n')
  const more = keys.length > 5 ? `\n… 共 ${keys.length} 个` : ''
  const warn = used.length
    ? `\n其中 ${used.length} 个仍被文章正文直接引用,删掉后这些位置会变成坏图/坏视频:\n` +
      used.map((o) => `· ${o.key}(被引用 ${o.ref_posts} 篇)`).join('\n')
    : ''
  try {
    await ElMessageBox.confirm(`从 S3 桶里删除选中的 ${keys.length} 个对象?此操作不可撤销。\n${preview}${more}${warn}`, '删除桶内对象', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  applySync({ delete_object_keys: keys }, '删除桶内对象')
}

const dropMissing = async () => {
  const rows = missPick.value
  if (!rows.length) return
  const used = rows.filter((r) => r.ref_posts > 0)
  const warn = used.length
    ? `\n其中 ${used.length} 条仍被文章引用(封面或正文),删除后这些位置会变成坏图:\n` +
      used.slice(0, 5).map((r) => `· #${r.id} ${r.filename}(引用 ${r.ref_posts} 篇)`).join('\n')
    : ''
  try {
    await ElMessageBox.confirm(`删除选中的 ${rows.length} 条悬空记录?桶内对象已不存在,只清数据库记录。${warn}`, '删除悬空记录', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  applySync({ delete_record_ids: rows.map((r) => r.id) }, '删除悬空记录')
}
</script>

<template>
  <div :class="{ 'media-pick-root': mode === 'pick' }" ref="rootRef">
    <div v-if="loading" style="display:grid;grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:14px">
      <el-skeleton v-for="i in 8" :key="i" animated>
        <template #template>
          <el-skeleton-item variant="image" style="height:150px;border-radius:12px" />
        </template>
      </el-skeleton>
    </div>
    <ErrorBox v-else-if="error" :error="error" retry @retry="load" />
    <div v-else-if="!data?.items.length">
      <p class="muted">{{ mode === 'pick' ? '媒体库为空,请先上传。' : '媒体库为空。' }}</p>
      <!-- 媒体库记录为空时也要能进入同步(桶里有对象、记录一条都没有,正是最需要同步的场景) -->
      <el-button v-if="mode === 'manage' && s3Storage" size="small" plain @click="openSync">同步 S3</el-button>
    </div>
    <template v-else>
      <!-- 批量工具栏 -->
      <div v-if="mode === 'manage'" class="media-batch">
        <el-checkbox
          class="batch-label"
          :model-value="data.items.every((i) => selected.has(i.id))"
          @change="toggleAll"
        >全选(本页)</el-checkbox>
        <span v-if="selected.size" class="muted">已选 {{ selected.size }} 项</span>
        <template v-if="selected.size">
          <el-button size="small" @click="batchDownload">下载</el-button>
          <el-button v-if="s3Storage" size="small" @click="batchTransfer">转移到 S3</el-button>
          <el-button size="small" type="danger" @click="batchDelete">删除</el-button>
          <el-button size="small" text @click="selected.clear()">取消</el-button>
        </template>
        <el-button size="small" plain style="margin-left: auto" v-if="s3Storage" @click="openSync">同步 S3</el-button>
      </div>

      <div :class="['media-grid', mode]" :style="mode === 'pick' ? { gridTemplateColumns: `repeat(${cols}, ${cardW}px)`, gridAutoRows: `${cardH}px`, justifyContent: 'center', alignContent: 'center' } : undefined">
        <div v-for="m in data.items" :key="m.id" class="media-card card" :class="{ selected: selected.has(m.id) }">
          <div class="media-thumb" :style="{ position: 'relative' }">
            <el-checkbox
              v-if="mode === 'manage'"
              class="media-check"
              :model-value="selected.has(m.id)"
              @click.stop
              @change="toggle(m.id)"
            />
            <!-- 缩略图点击 → Element Plus 图片预览(不再是开新标签) -->
            <div
              v-if="m.mime.startsWith('image/')"
              style="width: 100%; height: 100%; display: flex; cursor: zoom-in"
              @click="openViewer(m)"
            >
              <FocusImg :src="m.url" :alt="m.filename" />
            </div>
            <span v-else class="muted">{{ m.mime || '文件' }}</span>
          </div>
          <div class="media-name" :title="m.filename">{{ m.filename }}</div>
          <template v-if="mode === 'manage'">
            <div class="muted">{{ fmtBytes(m.size) }}{{ m.width && m.height ? ` · ${m.width}×${m.height}` : '' }}</div>
            <div class="muted">{{ fmtDateTime(m.created_at) }}</div>
          </template>
          <div style="display: flex; gap: 6px; align-items: center; flex-wrap: wrap">
            <el-tag type="info" size="small" effect="light">{{ m.storage }}</el-tag>
            <el-button v-if="mode === 'pick'" size="small" @click="emit('pick', m)">选为封面</el-button>
            <template v-else>
              <el-button v-if="focusActive && m.mime.startsWith('image/')" size="small" @click="openFocus(m)">选择焦点</el-button>
              <el-button v-if="m.mime.startsWith('image/')" size="small" @click="openViewer(m)">预览</el-button>
              <el-button size="small" tag="a" :href="m.url" target="_blank">打开</el-button>
              <el-button size="small" type="danger" @click="del(m)">删除</el-button>
            </template>
          </div>
        </div>
      </div>
      <el-pagination
        background
        layout="total, prev, pager, next"
        :page-size="PER"
        :total="data.total"
        :current-page="page"
        style="margin-top: 16px; justify-content: center; display: flex"
        @current-change="(n: number) => { page = n; load(); selected.clear() }"
      />
    </template>

    <FocusEditor v-if="focusTarget" :media="focusTarget" v-model:open="focusOpen" />

    <!-- 媒体库同步:扫描差异(只读)→ 勾选 → 二次确认 → 应用 -->
    <el-dialog v-model="syncOpen" title="媒体库同步(S3 桶 ↔ 媒体记录)" width="920px" top="6vh" append-to-body>
      <p class="muted" style="font-size: 12.5px; line-height: 1.75; margin: 0 0 12px">
        修复意外造成的不一致:直接从 S3 面板或其他工具传上去的文件不会进媒体库(<b>桶内孤儿</b>);
        桶内对象被误删后,媒体记录会变成<b>悬空记录</b>。扫描是只读的,所有修复动作都要先勾选、再确认。
      </p>

      <div style="display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 12px">
        <el-button type="primary" :loading="syncing" :disabled="applying" @click="doScan">扫描差异</el-button>
        <template v-if="scan">
          <el-tag type="info" effect="plain">桶 {{ scan.bucket }}</el-tag>
          <el-tag type="info" effect="plain">桶内对象 {{ scan.totals.bucket_objects }}</el-tag>
          <el-tag type="info" effect="plain">媒体记录 {{ scan.totals.records }} (s3 {{ scan.totals.s3_records }} / local {{ scan.totals.local_records }})</el-tag>
          <el-tag :type="scan.totals.orphans ? 'warning' : 'success'" effect="plain">桶内孤儿 {{ scan.totals.orphans }}</el-tag>
          <el-tag :type="scan.totals.missing ? 'warning' : 'success'" effect="plain">悬空记录 {{ scan.totals.missing }}</el-tag>
          <el-tag v-if="scan.totals.other_source" type="info" effect="plain">
            其他存储源 {{ scan.totals.other_source }}(不参与对账)
          </el-tag>
          <el-tag v-if="scan.totals.bucket_truncated" type="danger" effect="plain">对象过多,结果已截断</el-tag>
        </template>
      </div>

      <el-skeleton v-if="syncing && !scan" :rows="5" animated />
      <el-skeleton v-else-if="!scan" :rows="4" animated />
      <el-tabs v-else v-model="syncTab">
        <el-tab-pane :label="`桶内孤儿 (${scan.totals.orphans})`" name="orphan">
          <el-table
            v-if="scan.orphans.length"
            :data="scan.orphans"
            height="330"
            @selection-change="(rows: SyncObject[]) => (orphanPick = rows)"
          >
            <el-table-column type="selection" width="42" />
            <el-table-column prop="key" label="对象 key" min-width="250" show-overflow-tooltip />
            <el-table-column label="大小" width="92" align="right">
              <template #default="{ row }">{{ fmtBytes(row.size) }}</template>
            </el-table-column>
            <el-table-column prop="mime" label="类型" width="130" show-overflow-tooltip />
            <el-table-column label="内容策略" width="92" align="center">
              <template #default="{ row }">
                <el-tag :type="row.allowed ? 'success' : 'warning'" size="small" effect="light">
                  {{ row.allowed ? '符合' : '不符合' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="被引用" width="94" align="center">
              <template #default="{ row }">
                <el-tag :type="row.ref_posts ? 'danger' : 'info'" size="small" effect="light">
                  {{ row.ref_posts ? `${row.ref_posts} 篇` : '无' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="上传时间" width="150">
              <template #default="{ row }">{{ fmtDateTime(row.last_modified) }}</template>
            </el-table-column>
          </el-table>
          <el-empty v-else description="没有桶内孤儿:桶里的文件都已经登记进媒体库" :image-size="70" />
          <div style="margin-top: 12px; display: flex; gap: 8px; flex-wrap: wrap">
            <el-button type="primary" size="small" :disabled="!orphanPick.length || applying" @click="importOrphans">
              登记入库选中 ({{ orphanPick.length }})
            </el-button>
            <el-button type="danger" plain size="small" :disabled="!orphanPick.length || applying" @click="dropOrphans">
              删除桶内对象 ({{ orphanPick.length }})
            </el-button>
          </div>
        </el-tab-pane>

        <el-tab-pane :label="`悬空记录 (${scan.totals.missing})`" name="missing">
          <el-table
            v-if="scan.missing.length"
            :data="scan.missing"
            height="330"
            @selection-change="(rows: SyncRecord[]) => (missPick = rows)"
          >
            <el-table-column type="selection" width="42" />
            <el-table-column prop="id" label="#" width="68" />
            <el-table-column prop="filename" label="文件名" min-width="160" show-overflow-tooltip />
            <el-table-column prop="key" label="对象 key(桶内已不存在)" min-width="230" show-overflow-tooltip />
            <el-table-column label="大小" width="88" align="right">
              <template #default="{ row }">{{ fmtBytes(row.size) }}</template>
            </el-table-column>
            <el-table-column label="被引用" width="94" align="center">
              <template #default="{ row }">
                <el-tag :type="row.ref_posts ? 'danger' : 'info'" size="small" effect="light">
                  {{ row.ref_posts ? `${row.ref_posts} 篇` : '无' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="创建时间" width="150">
              <template #default="{ row }">{{ fmtDateTime(row.created_at) }}</template>
            </el-table-column>
          </el-table>
          <el-empty v-else description="没有悬空记录:全部 r2 记录在桶里都能找到对象" :image-size="70" />
          <div style="margin-top: 12px">
            <el-button type="danger" size="small" :disabled="!missPick.length || applying" @click="dropMissing">
              删除记录选中 ({{ missPick.length }})
            </el-button>
          </div>
        </el-tab-pane>

        <!-- 只读:指向历史桶/别的域名的记录。它们的对象不在当前桶里,URL 多半仍然有效,绝不能参与删除 -->
        <el-tab-pane v-if="scan.other_source.length" :label="`其他存储源 (${scan.totals.other_source})`" name="other">
          <p class="muted" style="font-size: 12.5px; line-height: 1.7; margin: 0 0 10px">
            这些记录的 URL 不属于当前配置的桶(<b>{{ scan.public_base }}</b>,桶 <b>{{ scan.bucket }}</b>),
            应是早期用了别的桶/域名。它们的对象不参与本次对账,
            <b>也不会在这里被删除</b>;若这些 URL 仍能正常访问,说明它们本来就是好的,无需处理。
          </p>
          <el-table :data="scan.other_source" height="300">
            <el-table-column prop="id" label="#" width="68" />
            <el-table-column prop="filename" label="文件名" min-width="150" show-overflow-tooltip />
            <el-table-column prop="url" label="URL(历史存储源)" min-width="300" show-overflow-tooltip />
            <el-table-column label="被引用" width="94" align="center">
              <template #default="{ row }">
                <el-tag :type="row.ref_posts ? 'danger' : 'info'" size="small" effect="light">
                  {{ row.ref_posts ? `${row.ref_posts} 篇` : '无' }}
                </el-tag>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </el-dialog>

    <!-- Element Plus 图片查看器(全屏预览) -->
    <el-image-viewer
      v-if="viewerOpen"
      :url-list="viewerUrls"
      :initial-index="viewerIndex"
      teleported
      hide-on-click-modal
      @close="viewerOpen = false"
    />
  </div>
</template>
