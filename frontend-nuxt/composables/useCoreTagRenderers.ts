// 核心内置渲染器登记表:核心的集成代码在 setup 期声明「这个落库标签我自己会渲染」,
// 账本回落(useTagFallbacks)据此跳过它,不需要核心为任何具体插件写死分支。
//
// 用 useState 而不是模块级变量: prerender 时模块状态会跨路由残留,
// 会让没挂载对应渲染器的页面误跳过回落。
export function useCoreTagRenderers() {
  const tags = useState<string[]>('fanke_core_tag_renderers', () => [])

  function claim(...names: string[]) {
    for (const n of names) if (!tags.value.includes(n)) tags.value.push(n)
  }

  const claimed = (tag: string) => tags.value.includes(tag)

  return { claim, claimed }
}
