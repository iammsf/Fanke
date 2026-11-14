<script setup lang="ts">
// 焦点区域选择器:在图片上叠加 ⊙ 中心点与 1:1 / 16:9 / 9:16 三个虚线框。
// 框与中心点均可拖动;每个框右下角有手柄可等比例缩放(不越出图片)。
// 数据全部以归一化坐标(0-1)存储,保存时走后端通用 store.save hook。
import type { FocusData, FocusRect, MediaItem } from '~/types'

const props = defineProps<{ media: MediaItem; open: boolean }>()
const emit = defineEmits<{ 'update:open': [boolean]; saved: [] }>()

const { api } = useAuth()
const { push } = useToast()
const { run } = useBusy()
const { invalidate } = useImageFocus()

const focus = ref<FocusData>(defaultFocus())
const loading = ref(false)
const saving = ref(false)
const imgEl = ref<HTMLImageElement>()
const natW = ref(0)
const natH = ref(0)

// 三类框:键名 / 视觉宽高比 / 颜色
const RECT_DEFS = [
  { key: 'square' as const, label: '1:1', ratio: 1, color: '#f6c343' },
  { key: 'wide' as const, label: '16:9', ratio: 16 / 9, color: '#42a5f5' },
  { key: 'tall' as const, label: '9:16', ratio: 9 / 16, color: '#ab47bc' },
]

// 由 w 求 h,使框在图片上的视觉比例 = ratio
const hFor = (w: number, ratio: number) => (natW.value && natH.value ? (w * natW.value) / (ratio * natH.value) : w)

// 修正某个框:h 跟随 w、整体夹回图片内
const fixRect = (r: FocusRect, ratio: number) => {
  r.w = Math.min(1, Math.max(0.04, r.w))
  r.h = hFor(r.w, ratio)
  if (r.h > 1) {
    r.h = 1
    r.w = (r.h * ratio * natH.value) / natW.value
  }
  r.x = Math.min(1 - r.w, Math.max(0, r.x))
  r.y = Math.min(1 - r.h, Math.max(0, r.y))
}

const onImgLoad = () => {
  natW.value = imgEl.value?.naturalWidth || 0
  natH.value = imgEl.value?.naturalHeight || 0
  for (const d of RECT_DEFS) fixRect(focus.value[d.key], d.ratio)
}

// ---------- 加载已有焦点 ----------
const load = async () => {
  loading.value = true
  try {
    const res = await api<{ data: any }>(
      `/admin/extensions/image-focus/hook/store.get`,
      { method: 'POST', body: JSON.stringify({ key: props.media.url }) },
    )
    if (res.data?.found && res.data.value) {
      focus.value = { ...defaultFocus(), ...res.data.value }
    } else {
      focus.value = defaultFocus()
    }
    onImgLoad() // 若图片已缓存完成,立即校正比例
  } catch (e: any) {
    push(e.message || '读取焦点数据失败', false)
  } finally {
    loading.value = false
  }
}
watch(
  () => props.open,
  (v) => {
    if (v) {
      focus.value = defaultFocus()
      load()
    }
  },
  // immediate: v-if 挂载时 open 可能已是 true(首次点击),不触发变化回调
  { immediate: true },
)

// ---------- 拖拽 ----------
type DragMode =
  | { kind: 'center' }
  | { kind: 'move'; key: 'square' | 'wide' | 'tall' }
  | { kind: 'scale'; key: 'square' | 'wide' | 'tall' }

const drag = ref<{
  mode: DragMode
  startX: number
  startY: number
  orig: FocusRect | { cx: number; cy: number }
} | null>(null)

const overlayBox = () => (imgEl.value?.parentElement as HTMLElement)?.getBoundingClientRect()

const startDrag = (e: PointerEvent, mode: DragMode) => {
  e.preventDefault()
  e.stopPropagation()
  const orig =
    mode.kind === 'center'
      ? { cx: focus.value.cx, cy: focus.value.cy }
      : { ...focus.value[(mode as any).key] }
  drag.value = { mode, startX: e.clientX, startY: e.clientY, orig }
  ;(e.target as HTMLElement).setPointerCapture?.(e.pointerId)
}

const onDrag = (e: PointerEvent) => {
  if (!drag.value) return
  const box = overlayBox()
  if (!box || !box.width || !box.height) return
  const dx = (e.clientX - drag.value.startX) / box.width
  const dy = (e.clientY - drag.value.startY) / box.height
  const { mode, orig } = drag.value
  const clamp01 = (v: number) => Math.min(1, Math.max(0, v))

  if (mode.kind === 'center') {
    const o = orig as { cx: number; cy: number }
    focus.value.cx = clamp01(o.cx + dx)
    focus.value.cy = clamp01(o.cy + dy)
    return
  }
  const def = RECT_DEFS.find((d) => d.key === (mode as any).key)!
  const r = focus.value[def.key]
  const o = orig as FocusRect
  if (mode.kind === 'move') {
    r.x = o.x + dx
    r.y = o.y + dy
  } else {
    r.w = Math.max(0.04, o.w + dx)
  }
  fixRect(r, def.ratio)
}

const endDrag = () => (drag.value = null)

// ---------- 重置某个框到默认位置 ----------
const resetRect = (key: 'square' | 'wide' | 'tall') => {
  const def = RECT_DEFS.find((d) => d.key === key)!
  focus.value[key] = { ...defaultFocus()[key] }
  fixRect(focus.value[key], def.ratio)
}

// ---------- 保存 ----------
const save = async () => {
  saving.value = true
  try {
    await run('保存中…', () => api(`/admin/extensions/image-focus/hook/store.save`, {
      method: 'POST',
      body: JSON.stringify({ key: props.media.url, value: focus.value }),
    }))
    invalidate(props.media.url)
    push('焦点已保存', true)
    emit('saved')
    emit('update:open', false)
  } catch (e: any) {
    push(e.message || '保存失败', false)
  } finally {
    saving.value = false
  }
}

const close = () => emit('update:open', false)
</script>

<template>
  <el-dialog :model-value="open" title="选择焦点" @update:model-value="close">
    <div class="fe" @pointermove="onDrag" @pointerup="endDrag" @pointercancel="endDrag">
      <p class="muted" style="margin: 0 0 8px; font-size: 13px">
        ⊙ 在圆圈内按住即可拖动;虚线框按住<b>虚线</b>拖动,右下角手柄等比例缩放,手柄旁的 ↺ 重置该框。选好后点右侧 √ 保存。
      </p>
      <div class="stage-wrap">
        <div class="stage">
          <img ref="imgEl" :src="media.url" :alt="media.filename" @load="onImgLoad" draggable="false" />
          <!-- ⊙ 中心点 -->
          <div
            class="center"
            :style="{ left: `${focus.cx * 100}%`, top: `${focus.cy * 100}%` }"
            title="中心点"
            @pointerdown="startDrag($event, { kind: 'center' })"
          >⊙</div>
          <!-- 三个取景框:容器不响应事件,只有四条虚线边缘可拖 -->
          <div
            v-for="d in RECT_DEFS"
            :key="d.key"
            class="rect"
            :style="{
              left: `${focus[d.key].x * 100}%`,
              top: `${focus[d.key].y * 100}%`,
              width: `${focus[d.key].w * 100}%`,
              height: `${focus[d.key].h * 100}%`,
              borderColor: d.color,
            }"
          >
            <span class="tag" :style="{ background: d.color }">{{ d.label }}</span>
            <span class="edge n" @pointerdown="startDrag($event, { kind: 'move', key: d.key })" />
            <span class="edge s" @pointerdown="startDrag($event, { kind: 'move', key: d.key })" />
            <span class="edge w" @pointerdown="startDrag($event, { kind: 'move', key: d.key })" />
            <span class="edge e" @pointerdown="startDrag($event, { kind: 'move', key: d.key })" />
            <span
              class="reset"
              :style="{ background: d.color }"
              title="重置该框"
              @pointerdown.stop
              @click.stop="resetRect(d.key)"
            >↺</span>
            <span
              class="handle"
              :style="{ background: d.color }"
              @pointerdown="startDrag($event, { kind: 'scale', key: d.key })"
            />
          </div>
        </div>
        <!-- 悬浮确认/取消 -->
        <div class="fab">
          <el-button type="primary" circle :loading="saving" title="保存" @click="save">✓</el-button>
          <el-button circle title="取消" @click="close">✕</el-button>
        </div>
      </div>
      <p v-if="loading" class="muted" style="margin: 8px 0 0; font-size: 13px">读取已有焦点…</p>
    </div>
  </el-dialog>
</template>

<style scoped>
.stage-wrap {
  position: relative;
  display: flex;
  gap: 10px;
  align-items: flex-start;
}
.stage {
  position: relative;
  display: inline-block;
  max-width: 100%;
  line-height: 0;
  user-select: none;
  touch-action: none;
}
.stage img {
  max-width: 100%;
  max-height: 66vh;
  display: block;
  border-radius: 8px;
  pointer-events: none;
}
.center {
  position: absolute;
  transform: translate(-50%, -50%);
  font-size: 26px;
  line-height: 1;
  color: #fff;
  text-shadow: 0 0 4px #000, 0 0 8px #000;
  cursor: grab;
  padding: 12px;
  z-index: 3;
  touch-action: none;
}
.center:active {
  cursor: grabbing;
}
/* 框容器只负责画虚线,不响应指针;命中区是四条边缘条 */
.rect {
  position: absolute;
  border: 2px dashed;
  border-radius: 4px;
  box-sizing: border-box;
  pointer-events: none;
  z-index: 1;
}
.rect .edge {
  position: absolute;
  pointer-events: auto;
  cursor: move;
  touch-action: none;
}
.rect .edge.n {
  left: 0;
  right: 0;
  top: -6px;
  height: 12px;
}
.rect .edge.s {
  left: 0;
  right: 0;
  bottom: -6px;
  height: 12px;
}
.rect .edge.w {
  top: 0;
  bottom: 0;
  left: -6px;
  width: 12px;
}
.rect .edge.e {
  top: 0;
  bottom: 0;
  right: -6px;
  width: 12px;
}
.rect .tag {
  position: absolute;
  top: -20px;
  left: -2px;
  color: #fff;
  font-size: 11px;
  line-height: 1;
  padding: 3px 6px;
  border-radius: 4px;
  white-space: nowrap;
  pointer-events: none;
}
.rect .handle {
  position: absolute;
  right: -8px;
  bottom: -8px;
  width: 15px;
  height: 15px;
  border-radius: 50%;
  cursor: nwse-resize;
  border: 2px solid #fff;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.4);
  pointer-events: auto;
  z-index: 5;
}
/* 重置按钮:缩放手柄正上方 */
.rect .reset {
  position: absolute;
  right: -8px;
  bottom: 12px;
  width: 15px;
  height: 15px;
  border-radius: 50%;
  color: #fff;
  font-size: 10px;
  line-height: 13px;
  text-align: center;
  cursor: pointer;
  border: 1px solid #fff;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.4);
  pointer-events: auto;
  z-index: 5;
}
.fab {
  display: flex;
  flex-direction: column;
  gap: 8px;
  position: sticky;
  top: 0;
}
.fab :deep(.el-button + .el-button) {
  margin-left: 0;
}
</style>
