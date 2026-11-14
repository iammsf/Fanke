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
 * 生成 srcset:全部指向缩图端点,绝不放原图。
 * 原图动辄几百 KB,一旦进 srcset,高分屏桌面就会选中它 —— 等于白做优化。
 * 原图只用于图片查看器打开大图(见 data-orig)。
 */
export function thumbSrcset(src: string, widths: number[], origW = 0): string {
  if (!src || src.startsWith('data:')) return ''
  const list = widths
    .filter((w) => (origW > 0 ? w < origW : true))
    .map((w) => `${thumbUrl(src, w)} ${w}w`)
  // 源图比所有档位都窄:用源图宽度当唯一档位(服务端只缩不放)
  if (!list.length && origW > 0) list.push(`${thumbUrl(src, origW)} ${origW}w`)
  return list.join(', ')
}

/** 卡片封面:桌面整宽(容器上限 1080px)、移动端 120px 定宽。 */
export const COVER_WIDTHS = [240, 480, 740, 1080]
export const COVER_SIZES = '(max-width: 640px) 120px, (max-width: 1100px) 92vw, 1080px'
