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

// image-viewer 插件脚本（TypeScript 源文件，安装时自动编译为 app.js）
// 逻辑与旧版 init/index.html 内联脚本保持一致，仅做类型化整理。

(function () {
  var $ = function (id) { return document.getElementById(id) }

  // 由快捷控件拼 des_sty 字符串
  function compose() {
    var t = []
    if ($('sHidden').checked) t.push('hidden')
    if ($('sBold').checked) t.push('b')
    if ($('sItalic').checked) t.push('i')
    if ($('sFs').value) t.push('fs=' + $('sFs').value)
    if ($('sC').value && $('sC').value !== '#6b7a8c') t.push('c=' + $('sC').value)
    if ($('sBg').value && $('sBg').value !== '#f4f6fb') t.push('bg=' + $('sBg').value)
    if ($('sBd').value && $('sBd').value !== '#e2e8f2') t.push('bd=' + $('sBd').value)
    if ($('sW').value) t.push('w=' + $('sW').value)
    if ($('sR').value) t.push('r=' + $('sR').value)
    return t.join(',')
  }

  // 解析 des_sty 字符串回填快捷控件
  function parseInto(str) {
    $('sHidden').checked = $('sBold').checked = $('sItalic').checked = false
    $('sFs').value = ''; $('sW').value = ''; $('sR').value = ''
    $('sC').value = '#6b7a8c'; $('sBg').value = '#f4f6fb'; $('sBd').value = '#e2e8f2'
    ;(str || '').split(/[,\s;]+/).forEach(function (tok) {
      if (!tok) return
      if (tok === 'hidden') $('sHidden').checked = true
      else if (tok === 'b') $('sBold').checked = true
      else if (tok === 'i') $('sItalic').checked = true
      else {
        var kv = tok.split('=')
        if (kv.length !== 2) return
        var k = kv[0], v = kv[1]
        if (k === 'fs') $('sFs').value = v
        else if (k === 'c') $('sC').value = v
        else if (k === 'bg') $('sBg').value = v
        else if (k === 'bd') $('sBd').value = v
        else if (k === 'w') $('sW').value = v
        else if (k === 'r') $('sR').value = v
      }
    })
  }

  function preview() {
    var pos = $('posH').value + ' ' + $('posV').value
    var sty = $('styRaw').value
    var fig = $('pvFig'), cap = $('pvCap'), img = fig.querySelector('img')
    // 位置
    fig.classList.toggle('pv-top', pos.indexOf('top') >= 0)
    cap.style.textAlign = pos.indexOf('left') >= 0 ? 'left' : pos.indexOf('right') >= 0 ? 'right' : 'center'
    // 样式
    cap.style.display = ''
    cap.style.fontWeight = ''; cap.style.fontStyle = ''
    cap.style.fontSize = ''; cap.style.color = ''; cap.style.background = ''; cap.style.border = ''
    img.style.width = '220px'; img.style.borderRadius = '8px'
    ;(sty || '').split(/[,\s;]+/).forEach(function (tok) {
      if (!tok) return
      if (tok === 'hidden') cap.style.display = 'none'
      else if (tok === 'b') cap.style.fontWeight = '700'
      else if (tok === 'i') cap.style.fontStyle = 'italic'
      else {
        var kv = tok.split('=')
        if (kv.length !== 2) return
        var k = kv[0], v = kv[1]
        if (k === 'fs') cap.style.fontSize = v + 'px'
        else if (k === 'c') cap.style.color = v
        else if (k === 'bg') cap.style.background = v
        else if (k === 'bd') cap.style.border = '1px solid ' + v
        else if (k === 'w') img.style.width = (v * 2.2) + 'px'
        else if (k === 'r') img.style.borderRadius = v + 'px'
      }
    })
  }

  function syncFromControls() { $('styRaw').value = compose(); preview() }
  ;['sHidden','sBold','sItalic','sFs','sC','sBg','sBd','sW','sR'].forEach(function (id) {
    $(id).addEventListener('input', syncFromControls)
  })
  $('styRaw').addEventListener('input', function () { parseInto($('styRaw').value); preview() })
  $('posV').addEventListener('change', preview)
  $('posH').addEventListener('change', preview)

  function load() {
    Fanke.callHook('store.get', { key: 'config' }).then(function (d) {
      var v = (d && d.found && d.value) || {}
      var pos = (v.des_pos || 'middle bottom').split(/\s+/)
      $('posV').value = pos.indexOf('top') >= 0 ? 'top' : 'bottom'
      $('posH').value = pos.indexOf('left') >= 0 ? 'left' : pos.indexOf('right') >= 0 ? 'right' : 'middle'
      $('styRaw').value = v.des_sty || ''
      $('replaceImg').checked = !!v.replace_img
      parseInto($('styRaw').value)
      preview()
      $('stateTip').textContent = d && d.found ? '已读取已保存配置' : '尚无保存配置,当前为默认值'
    }).catch(function (e) {
      Fanke.emit('toast', { message: '读取失败: ' + (e.message || e), ok: false })
    })
  }

  $('saveBtn').addEventListener('click', function () {
    var val = { des_pos: $('posH').value + ' ' + $('posV').value, des_sty: $('styRaw').value.trim(), replace_img: $('replaceImg').checked }
    Fanke.callHook('store.save', { key: 'config', value: val }).then(function () {
      Fanke.emit('toast', { message: '配置已保存' })
      $('stateTip').textContent = '已保存 ' + new Date().toLocaleTimeString()
    }).catch(function (e) {
      Fanke.emit('toast', { message: '保存失败: ' + (e.message || e), ok: false })
    })
  })
  $('loadBtn').addEventListener('click', load)

  load()
})()
