// 图片缩图地址工具。
//
// 服务端 /img 端点:?src=<原图>&w=<目标宽度>,命中缓存直接返回 WebP,否则现场转码。
// 前端所有图片都统一走这里,浏览器不再直连第三方图床,也不再下载几倍于显示尺寸的原图。

/** 单张图的缩图地址。 */
export function thumbUrl(src: string, w: number): string {
  if (!src || src.startsWith('data:')) return src
  return `/img?src=${encodeURIComponent(src)}&w=${w}`
}

/**
 * 生成 srcset:小档走缩图端点,原图作为最大候选放在末尾。
 * 原图只有高分屏(2x/3x)真的需要那么多像素时才会被选中,
 * 否则小图会被强行放大显示,文字和截图会明显发虚。
 */
export function thumbSrcset(src: string, widths: number[], origW = 0): string {
  if (!src || src.startsWith('data:')) return ''
  const list = widths
    .filter((w) => (origW > 0 ? w < origW : true))
    .map((w) => `${thumbUrl(src, w)} ${w}w`)
  // 源图比所有档位都窄:用源图宽度当唯一档位(服务端只缩不放)
  if (!list.length && origW > 0) list.push(`${thumbUrl(src, origW)} ${origW}w`)
  // 原图作为最大候选,供高 DPR 屏幕取用
  if (origW > 0 && !list.some((s) => s.endsWith(` ${origW}w`))) list.push(`${src} ${origW}w`)
  return list.join(', ')
}

/** 卡片封面:桌面整宽(容器上限 1080px)、移动端 120px 定宽。 */
export const COVER_WIDTHS = [240, 480, 740, 1080, 1440]
export const COVER_SIZES = '(max-width: 640px) 120px, (max-width: 1100px) 92vw, 1080px'
