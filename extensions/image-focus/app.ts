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

// image-focus 插件脚本（TypeScript 源文件，安装时自动编译为 app.js）
// 逻辑与旧版 init/index.html 内联脚本保持一致，仅做类型化整理。

(function () {
  var $ = function (id) { return document.getElementById(id) }
  var hook = function (name, payload) { return Fanke.callHook(name, payload || {}) }
  var toast = function (msg, ok) { Fanke.emit('toast', { message: msg, ok: ok !== false }) }

  // ---------- 统计 ----------
  function refreshStats() {
    hook('store.stats').then(function (d) {
      $('statTotal').textContent = d.total
    }).catch(function () { $('statTotal').textContent = '表未就绪' })
    hook('media.list', { mime_prefix: 'image/' }).then(function (d) {
      $('statMedia').textContent = d.total
    }).catch(function () { $('statMedia').textContent = '—' })
  }

  // 与前端一致的默认焦点:正中心,各框居中
  function defaultFocus() {
    return {
      cx: 0.5, cy: 0.5,
      square: { x: 0.25, y: 0.25, w: 0.5, h: 0.5 },
      wide: { x: 0.1, y: 0.21875, w: 0.8, h: 0.45 },
      tall: { x: 0.275, y: 0.1, w: 0.45, h: 0.8 }
    }
  }

  // ---------- 扫描 ----------
  $('scanBtn').addEventListener('click', function () {
    var btn = this
    btn.disabled = true
    $('scanMsg').textContent = '读取媒体库…'
    hook('media.list', { mime_prefix: 'image/' }).then(function (d) {
      var urls = (d.items || []).map(function (m) { return m.url })
      if (!urls.length) {
        $('scanMsg').textContent = '媒体库暂无图片'
        return
      }
      $('scanMsg').textContent = '写入默认焦点(' + urls.length + ' 张)…'
      var items = urls.map(function (u) { return { key: u, value: defaultFocus() } })
      // overwrite:false = 已有焦点记录不动,只补缺
      return hook('store.import', { items: items, overwrite: false }).then(function (r) {
        $('scanMsg').textContent = '完成:新初始化 ' + r.imported + ' 张,已存在 ' + r.skipped + ' 条跳过'
        toast('媒体库扫描完成')
        refreshStats()
      })
    }).catch(function (e) {
      $('scanMsg').textContent = '扫描失败:' + e.message
    }).finally(function () { btn.disabled = false })
  })

  // ---------- 导出 ----------
  $('exportBtn').addEventListener('click', function () {
    var btn = this
    btn.disabled = true
    hook('store.export').then(function (d) {
      var blob = new Blob([JSON.stringify(d, null, 2)], { type: 'application/json' })
      var a = document.createElement('a')
      a.href = URL.createObjectURL(blob)
      a.download = 'image-focus-export-' + new Date().toISOString().slice(0, 10) + '.json'
      a.click()
      URL.revokeObjectURL(a.href)
      $('manageMsg').textContent = '已导出 ' + d.count + ' 条'
    }).catch(function (e) {
      $('manageMsg').textContent = '导出失败:' + e.message
    }).finally(function () { btn.disabled = false })
  })

  // ---------- 导入 ----------
  $('importBtn').addEventListener('click', function () { $('importFile').click() })
  $('importFile').addEventListener('change', function () {
    var f = this.files && this.files[0]
    if (!f) return
    var reader = new FileReader()
    reader.onload = function () {
      var json
      try { json = JSON.parse(reader.result) } catch (e) {
        $('manageMsg').textContent = '文件不是合法 JSON'
        return
      }
      var items = json && (json.items || json)
      if (!Array.isArray(items)) {
        $('manageMsg').textContent = '格式错误:期望 {items:[{key,value}...]}'
        return
      }
      // 兼容旧版导出格式 {url, focus}
      items = items.map(function (it) {
        if (it && it.key !== undefined) return { key: it.key, value: it.value }
        if (it && it.url !== undefined) return { key: it.url, value: it.focus }
        return { key: '', value: null }
      })
      hook('store.import', { items: items }).then(function (d) {
        $('manageMsg').textContent = '导入完成:' + d.imported + ' 条' + (d.skipped ? ',跳过 ' + d.skipped + ' 条' : '')
        toast('导入完成')
        refreshStats()
      }).catch(function (e) {
        $('manageMsg').textContent = '导入失败:' + e.message
      })
    }
    reader.readAsText(f)
    this.value = ''
  })

  // ---------- 去重 ----------
  $('dedupBtn').addEventListener('click', function () {
    var btn = this
    btn.disabled = true
    hook('store.dedup').then(function (d) {
      $('manageMsg').textContent = '去重完成:移除 ' + d.removed + ' 条,现存 ' + d.total + ' 条'
      refreshStats()
    }).catch(function (e) {
      $('manageMsg').textContent = '去重失败:' + e.message
    }).finally(function () { btn.disabled = false })
  })

  // ---------- 无效链接检测 ----------
  var badUrls = []

  // 浏览器侧探测:<img> 加载成功即浏览器可达
  function browserProbe(url) {
    return new Promise(function (resolve) {
      var done = false
      var finish = function (ok) { if (!done) { done = true; resolve(ok) } }
      var img = new Image()
      img.onload = function () { finish(true) }
      img.onerror = function () { finish(false) }
      setTimeout(function () { finish(false) }, 6000)
      img.src = url
    })
  }

  // 并发池
  function pool(items, size, worker, onStep) {
    var i = 0
    var results = {}
    return new Promise(function (resolve) {
      var running = 0
      function next() {
        while (running < size && i < items.length) {
          ;(function (item) {
            i++; running++
            worker(item).then(function (r) {
              results[item] = r
              running--
              if (onStep) onStep()
              next()
            })
          })(items[i])
        }
        if (i >= items.length && running === 0) resolve(results)
      }
      next()
    })
  }

  $('checkBtn').addEventListener('click', function () {
    var btn = this
    btn.disabled = true
    badUrls = []
    $('badList').classList.add('hidden')
    $('badActions').classList.add('hidden')
    $('checkProgWrap').classList.remove('hidden')
    $('checkProg').style.width = '0%'
    $('checkMsg').textContent = '读取记录…'

    hook('store.list').then(function (d) {
      var urls = (d.items || []).map(function (it) { return it.key })
      if (!urls.length) {
        $('checkMsg').textContent = '暂无记录'
        btn.disabled = false
        $('checkProgWrap').classList.add('hidden')
        return
      }
      var total = urls.length
      var doneCount = 0
      var step = function () {
        doneCount++
        $('checkProg').style.width = Math.round(doneCount / total * 100) + '%'
        $('checkMsg').textContent = '检测中 ' + doneCount + '/' + total
      }

      // 后端分批(≤100/批)
      var backendMap = {}
      var batches = []
      for (var i = 0; i < urls.length; i += 100) batches.push(urls.slice(i, i + 100))
      var backendP = batches.reduce(function (p, batch) {
        return p.then(function () {
          return hook('http.check_url', { urls: batch }).then(function (r) {
            Object.assign(backendMap, r.reachable || {})
          })
        })
      }, Promise.resolve())

      // 浏览器逐条(并发 8)
      var browserP = pool(urls, 8, browserProbe, step).then(function (m) { return m })

      Promise.all([backendP, browserP]).then(function (rs) {
        var browserMap = rs[1]
        badUrls = urls.filter(function (u) {
          var bOk = !!browserMap[u]
          var sOk = !!backendMap[u]
          return !bOk && !sOk // 双不可达才无效
        })
        $('checkProgWrap').classList.add('hidden')
        if (!badUrls.length) {
          $('checkMsg').innerHTML = '<span class="ok">全部链接有效(' + total + ' 条)</span>'
          toast('链接检测完成,全部有效')
        } else {
          $('checkMsg').innerHTML = '<span class="warn">发现 ' + badUrls.length + ' / ' + total + ' 条无效链接</span>'
          $('badList').innerHTML = badUrls.map(function (u) {
            var d = document.createElement('div')
            d.textContent = u
            return d.outerHTML
          }).join('')
          $('badList').classList.remove('hidden')
          $('badActions').classList.remove('hidden')
        }
        btn.disabled = false
      }).catch(function (e) {
        $('checkMsg').textContent = '检测失败:' + e.message
        $('checkProgWrap').classList.add('hidden')
        btn.disabled = false
      })
    }).catch(function (e) {
      $('checkMsg').textContent = '读取记录失败:' + e.message
      btn.disabled = false
    })
  })

  $('deleteBadBtn').addEventListener('click', function () {
    if (!badUrls.length) return
    if (!confirm('确认删除 ' + badUrls.length + ' 条无效焦点记录?此操作不可恢复。')) return
    var btn = this
    btn.disabled = true
    hook('store.delete', { keys: badUrls }).then(function (d) {
      $('checkMsg').textContent = '已删除 ' + d.deleted + ' 条'
      $('badList').classList.add('hidden')
      $('badActions').classList.add('hidden')
      toast('已清理无效记录')
      refreshStats()
    }).catch(function (e) {
      $('checkMsg').textContent = '删除失败:' + e.message
    }).finally(function () { btn.disabled = false })
  })
  $('cancelBadBtn').addEventListener('click', function () {
    $('badList').classList.add('hidden')
    $('badActions').classList.add('hidden')
  })

  // 页面加载即刷新统计
  if (window.Fanke) refreshStats()
})()
