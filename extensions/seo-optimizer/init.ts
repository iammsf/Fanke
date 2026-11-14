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

(async function () {
  if (!window.Fanke) return
  try {
    await Fanke.callHook('store.ensure_table')
    const current = await Fanke.callHook('store.get', { key: 'seo_config' })
    if (!current.found) {
      await Fanke.callHook('store.save', {
        key: 'seo_config',
        value: {
          min_title_length: 10,
          max_title_length: 60,
          min_description_length: 50,
          max_description_length: 160
        }
      })
    }
    Fanke.initDone()
  } catch (e) {
    Fanke.emit('toast', { message: '初始化失败: ' + (e.message || e), ok: false })
  }
})()
