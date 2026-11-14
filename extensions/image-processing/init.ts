// 宿主注入的桥接 API（仅类型声明，编译后不产生运行时代码）
interface FankeBridge {
  callHook: (name: string, payload?: unknown) => Promise<any>
  emit: (name: string, payload?: unknown) => void
  initDone: () => void
  isInit?: boolean
  [key: string]: any
}
interface Window {
  Fanke?: FankeBridge
}

(function () {
  if (!window.Fanke) return
  Fanke.initDone()
  Fanke.emit('toast', { message: '图片处理插件初始化完成，请在插件详情页配置处理参数', ok: true })
})()
