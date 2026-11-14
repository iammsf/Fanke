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

// 图片插入/展示组件 · init.js
// 首次安装时由后端注入执行(Fanke.isInit === true 的页面加载中):
// 1) 调用 store.ensure_table 创建本插件的数据表(幂等)
// 2) 写入默认展示配置(key = "config")
// 3) 通知宿主初始化完成(Fanke.initDone)
(async function () {
  if (!window.Fanke) return
  try {
    await Fanke.callHook('store.ensure_table')
    await Fanke.callHook('store.save', {
      key: 'config',
      value: { des_pos: 'middle bottom', des_sty: '', replace_img: false },
    })
    Fanke.emit('toast', { message: '数据表与默认配置已就绪', ok: true })
  } catch (e) {
    Fanke.emit('toast', { message: '初始化失败: ' + (e.message || e), ok: false })
    return
  }
  Fanke.initDone()
})()
