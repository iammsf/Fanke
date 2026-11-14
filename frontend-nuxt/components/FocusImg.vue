<script setup lang="ts">
// 焦点感知 <img>:image-focus 插件启用时,按容器宽高比选用对应取景框/中心点,
// 结合图片实际比例精确计算 object-position,保证缩略图裁切时保留图片重点。
// 未启用/无数据时行为等同普通 <img>(object-position 默认 50% 50%)。
//
// 加载时序(用户要求的顺序):
//   1) 渲染骨架屏(流光渐变,此时 <img> 挂 1×1 透明像素,浏览器绝不会画"碎图"图标或替代文字)
//   2) 等焦点数据落地(插件启用状态 → store.get_batch → 该图焦点)
//   3) 就绪后落下真实 src + 最终 object-position → 显示图片;
//      若图片确实取不到(探测失败/加载失败)→ 换成"资源丢失"占位图,而不是浏览器碎图图标。
//
// 为什么占位期不把 src 置空:src 属性缺席/为空时,浏览器会在元素里画破图图标 + alt 替代文字
// (卡片封面就会先出现"裂开的图 + 文章标题"),所以改成挂一个必定加载成功的透明像素,再由类名盖骨架渐变。
//
// 尺寸来源:焦点定位需要图片自然宽高,占位期 DOM 上的 <img> 只是透明像素,
//   因此用 new Image() 并行预取真实图(顺便预热点缓存)。预取必须与门控并行、不能被门控挡住,
//   否则会死锁:pos 需要 imgSize,imgSize 需要预取,而门控又要等 pos。
const props = defineProps<{
  src: string
  alt?: string
  imgClass?: string
  loading?: 'lazy' | 'eager'
  // 响应式图源(sizes/srcset):默认按卡片封面档位;不需要传 widths 时留空即退化为单图地址
  widths?: number[]
  sizes?: string
  // 图片原始宽高(已知时写上,彻底消除布局抖动)
  width?: number
  height?: number
  // priority:首屏关键图(如首页前几张封面)—— 用 eager + fetchpriority=high + preload 预热
  // 让字节尽早下载,但**显示仍走焦点门控**:首次出现就必须在焦点位置,绝不先画再跳
  priority?: boolean
}>()

const { isEnabled, ensurePublic, loaded } = useExtensions()
const { focusOf, prefetch, isResolved } = useImageFocus()

const active = computed(() => isEnabled('image-focus'))
if (import.meta.client) void ensurePublic()

// 插件启用即排队拉取本图焦点数据:不依赖计算属性的求值顺序,保证数据不会晚于显示门控触发
watch(
  [active, () => props.src],
  () => {
    if (active.value) prefetch([props.src])
  },
  { immediate: true },
)

const el = ref<HTMLImageElement>()
const containerSize = ref({ w: 0, h: 0 })
const imgSize = ref({ w: 0, h: 0 })
const painted = ref(false) // 真实图片已下载完成(可绘制)
const imgFailed = ref(false) // 图片确实取不到(预取失败或 DOM 加载失败)
let ro: ResizeObserver | null = null

const markLoaded = (w: number, h: number) => {
  if (w && h) imgSize.value = { w, h }
  painted.value = true
}

// 响应式图源:srcset/sizes 只在浏览器支持且配置了档位时启用
const srcset = computed(() => {
  const ws = props.widths
  if (!ws?.length || !props.src) return ''
  return thumbSrcset(props.src, ws, props.width || 0)
})

// 并行预取:拿自然宽高(焦点定位要用),并提前把图塞进缓存(门控放行后即刻绘出)。
// 已知原始宽高时完全不预取 —— 预取只能命中"最小一档",而浏览器实际会按 sizes 选档,
// 两者不同就会白下一份(实测首页每张卡片因此多传一份 240w),等于凭空多 10 个请求。
watch(
  () => props.src,
  (src) => {
    painted.value = false
    imgFailed.value = false
    if (props.width && props.height) {
      imgSize.value = { w: props.width, h: props.height }
      return
    }
    imgSize.value = { w: 0, h: 0 }
    if (!import.meta.client || !src) return
    const probe = new Image()
    probe.onload = () => {
      if (props.src === src) markLoaded(probe.naturalWidth, probe.naturalHeight)
    }
    probe.onerror = () => {
      if (props.src === src) imgFailed.value = true
    }
    probe.src = props.widths?.length ? thumbUrl(src, props.widths[0]!) : src
  },
  { immediate: true },
)

const onImgLoad = () => {
  // 只有真实图落地后才采信尺寸:占位透明像素(1×1)不得污染 imgSize,否则焦点定位会按错误比例算
  if (!el.value || !showingReal.value) return
  markLoaded(el.value.naturalWidth, el.value.naturalHeight)
}

onMounted(() => {
  if (!el.value) return
  const updateContainer = () => {
    const r = el.value!.getBoundingClientRect()
    containerSize.value = { w: r.width, h: r.height }
  }
  updateContainer()
  ro = new ResizeObserver(updateContainer)
  ro.observe(el.value)
})
onBeforeUnmount(() => {
  ro?.disconnect()
})

const focus = computed(() => (active.value ? focusOf(props.src).value : null))
const pos = computed(() => {
  if (!focus.value) return ''
  const cw = containerSize.value.w
  const ch = containerSize.value.h
  const iw = imgSize.value.w
  const ih = imgSize.value.h
  if (!cw || !ch || !iw || !ih) return ''
  return objectPositionFor(focus.value, cw / ch, iw / ih)
})

// 显示门控:焦点数据尚未落地时不落 src,避免图片先按默认位置画出来再跳到焦点位置。
// 对 priority 一视同仁 —— priority 只提前"取字节"(preload 预热),不跳过"何时落笔"。
const ready = computed(() => {
  if (!loaded.value) return false // 插件启用状态未知
  if (!active.value) return true // 插件未启用:等同普通图片
  if (!isResolved(props.src)) return false // 焦点数据尚未落地
  return focus.value === null || pos.value !== '' // 有焦点则等尺寸算完 object-position
})

// 占位期挂 1×1 透明像素:地址必定加载成功,浏览器不会画破图图标,也不会把 alt 画成替代文字
const PLACEHOLDER_PIXEL =
  'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7'
const showingReal = computed(() => ready.value && !imgFailed.value)
// 有 srcset 时 src 只是兜底候选:同样指向缩图,绝不因为兜底再把几百 KB 原图拉一遍
const fallbackSrc = computed(() => (srcset.value ? thumbUrl(props.src, 740) : props.src))
const shownSrc = computed(() => (showingReal.value ? fallbackSrc.value : PLACEHOLDER_PIXEL))

// 占位形态:sk = 骨架屏(等门控/等图片),lost = 资源丢失占位图,none = 正常显示图片
const phase = computed<'sk' | 'lost' | 'none'>(() => {
  if (imgFailed.value) return 'lost'
  if (!ready.value) return 'sk'
  if (!painted.value) return 'sk' // 真实图尚未画出来:一律骨架(priority 也不例外,保证首次出现即在焦点位置)
  return 'none'
})
// srcset 必须与真实 src 同时下发:占位期若给出候选,浏览器会绕过门控提前绘出错误取景
const shownSrcset = computed(() => (ready.value ? srcset.value : ''))

// priority 图的预热:门控等待焦点期间,用 preload 按同一份 srcset/sizes 抢先拉取
// 浏览器最终会选中的那一档(与 <img> 同一套选档逻辑 → 必命中同一缓存,不会白下第二份)。
// 门控放行时 src 落地即从缓存绘出 —— 早加载与"首次出现即在焦点位置"兼得。
useHead(() => {
  if (!props.priority || !props.src || !import.meta.client) return {}
  return {
    link: [
      {
        rel: 'preload',
        as: 'image',
        href: fallbackSrc.value || props.src,
        ...(srcset.value ? { imagesrcset: srcset.value, imagesizes: props.sizes } : {}),
      },
    ],
  }
})
const imgStyle = computed(() => (pos.value ? { objectPosition: pos.value } : undefined))
// 未显示真实图片前不给 alt:否则浏览器会在空框里把标题当"替代文字"画出来,看着就像碎图
const imgAlt = computed(() => (painted.value && !imgFailed.value ? props.alt || '' : ''))
</script>

<template>
  <img
    ref="el"
    :src="shownSrc"
    :srcset="shownSrcset || undefined"
    :sizes="shownSrcset ? sizes : undefined"
    :width="width || undefined"
    :height="height || undefined"
    :alt="imgAlt"
    :class="[imgClass, phase === 'sk' ? 'fimg-sk' : '', phase === 'lost' ? 'fimg-lost' : '']"
    :loading="priority ? 'eager' : loading || 'lazy'"
    :fetchpriority="priority ? 'high' : undefined"
    decoding="async"
    :style="imgStyle"
    @load="onImgLoad"
    @error="imgFailed = true"
  />
</template>

<style scoped>
/* 骨架屏:直接沿用 Element Plus 骨架的配方(同一组令牌 + 同一条渐变曲线),
   底色为实心灰、亮暗两侧都是灰,不会像之前的「浅灰→纯白」那样糊成一片白。
   令牌来自主题:浅色 #f0f2f5↔#e6e8eb,深色 #303030↔#424243。 */
.fimg-sk,
.fimg-lost {
  width: 100%; /* 占位期撑满容器,避免 1×1 透明像素把布局压成一条线 */
  height: 100%;
}
.fimg-sk {
  background-color: var(--el-fill-color);
  background-image: linear-gradient(
    90deg,
    var(--el-fill-color) 25%,
    var(--el-fill-color-darker) 37%,
    var(--el-fill-color) 63%
  );
  background-size: 400% 100%;
  animation: fimg-shine 1.4s ease-in-out infinite;
}
@keyframes fimg-shine {
  0% {
    background-position: 100% 50%;
  }
  100% {
    background-position: 0 50%;
  }
}
/* 资源丢失占位图:内联 SVG 画一个"破图"符号,底色同骨架,描边在明暗主题下都可见 */
.fimg-lost {
  background-color: var(--el-fill-color);
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='48' height='48' viewBox='0 0 24 24'%3E%3Crect x='2.5' y='5' width='19' height='14' rx='2.2' fill='none' stroke='%238a94a3' stroke-width='1.4'/%3E%3Cpath d='M5.2 16.2l4.1-4.4 2.9 3.1 2.6-2.3 4.1 4.6' fill='none' stroke='%238a94a3' stroke-width='1.4' stroke-linejoin='round'/%3E%3Ccircle cx='8.5' cy='9.4' r='1.3' fill='%238a94a3'/%3E%3Cpath d='M3.6 20.6L20.4 3.4' stroke='%238a94a3' stroke-width='1.5' stroke-linecap='round'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: center;
  background-size: 44px 44px;
}
</style>
