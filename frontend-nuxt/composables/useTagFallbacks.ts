// 插件标签回落账本的渲染端入口。账本与「在线插件」都来自 /ext 的共享缓存
// (见 useExtensions),文章页与后台编辑器共用,不额外发请求。
//
// 插件被禁用、卸载或文件损坏时它就不在 enabled 里,正文里已写进去的标签自动按账本
// 模板渲染,不需要核心为每个插件写死回落分支。
import { applyFallbacks, wrapFallbacks } from '~/utils/tagFallback'

export function useTagFallbacks() {
  const { enabled, fallbacks, ensurePublic } = useExtensions()
  const { claimed } = useCoreTagRenderers()

  const load = ensurePublic

  const isDead = (slug: string) => !enabled.value.includes(slug)

  /** 文章页:不在线且无核心专用渲染器的标签按账本模板渲染;其余原样交给后续渲染器。 */
  function render(html: string): string {
    return applyFallbacks(html, fallbacks.value, (rec) => !isDead(rec.slug) && claimed(rec.tag))
  }

  /**
   * 编辑器画布:不在线插件的落库标签包成信封节点(视觉 = 账本模板,原文 = data-fanke-orig),
   * 保存时由 unwrap 原样还原,插件停用期间编辑正文不会丢标签。
   * 已注册到编辑器的标签(插件在线)交给插件自己的 toEditor,这里不碰。
   */
  function wrap(html: string, registered: (tag: string) => boolean): string {
    return wrapFallbacks(html, fallbacks.value, (rec) => isDead(rec.slug) && !registered(rec.tag))
  }

  return { load, render, wrap, isDead }
}
