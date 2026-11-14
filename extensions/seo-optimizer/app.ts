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

// seo-optimizer 插件脚本（TypeScript 源文件，安装时自动编译为 app.js）
// 逻辑与旧版 init/index.html 内联脚本保持一致，仅做类型化整理。

const defaultConfig = {
  min_title_length: 10,
  max_title_length: 60,
  min_description_length: 50,
  max_description_length: 160
}

let config = { ...defaultConfig }
let editorState = { title: '', summary: '', content: '', selection: '', post_id: null }
let lastResult = null
let aiBusy = false
// 已应用的建议:记录改前原值,支持撤销;同一字段只保留一条
let appliedState = { title: null, summary: null }
let polishBusy = false
let polishApplied = null
let polishSource = ''

const isPanel = new URLSearchParams(location.search).get('panel') === '1'

function $(id) { return document.getElementById(id) }

function showToast(msg, ok) {
  const toast = $('toast')
  toast.textContent = msg
  toast.className = 'toast show ' + (ok ? 'ok' : 'error')
  setTimeout(() => toast.classList.remove('show'), 3000)
}

function notify(msg, ok) {
  if (isPanel) Fanke.emit('toast', { message: msg, ok: ok !== false })
  else showToast(msg, ok)
}

// 通知用户:优先走宿主 notify.send(可弹窗展示多行错误),失败再回落到本地 toast
async function notifyUser(level, title, message, scope) {
  try {
    await Fanke.callHook('notify.send', {
      level: level,
      title: title,
      message: message,
      scope: scope || 'global'
    })
    return
  } catch (e) {
    notify(message, level === 'success' || level === 'info')
  }
}

// ---------- 配置 ----------
async function loadConfig() {
  try {
    const saved = await Fanke.callHook('store.get', { key: 'seo_config' })
    if (saved.found) {
      const value = typeof saved.value === 'string' ? JSON.parse(saved.value) : saved.value
      config = { ...defaultConfig, ...value }
    }
  } catch (e) {
    notify('加载配置失败: ' + (e.message || e), false)
  }
}

function updateSettingsUI() {
  if (isPanel) return
  $('minTitleLength').value = config.min_title_length
  $('maxTitleLength').value = config.max_title_length
  $('minDescLength').value = config.min_description_length
  $('maxDescLength').value = config.max_description_length
}

async function saveConfig() {
  const next = {
    min_title_length: Number($('minTitleLength').value),
    max_title_length: Number($('maxTitleLength').value),
    min_description_length: Number($('minDescLength').value),
    max_description_length: Number($('maxDescLength').value)
  }
  for (const [key, value] of Object.entries(next)) {
    const limit = key.includes('title') ? 100 : 500
    if (!Number.isInteger(value) || value < 1 || value > limit) throw new Error('长度必须是范围内的整数')
  }
  if (next.min_title_length > next.max_title_length || next.min_description_length > next.max_description_length) {
    throw new Error('最小长度不能大于最大长度')
  }
  await Fanke.callHook('store.save', { key: 'seo_config', value: next })
  config = next
  notify('配置已保存', true)
}

// ---------- 本地分析 ----------
const stripTags = (html) => (html || '').replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim()

function analyze() {
  const title = editorState.title || ''
  const summary = editorState.summary || ''
  const content = editorState.content || ''

  const r = {
    titleLength: Array.from(title).length,
    titleIssues: [],
    descriptionLength: Array.from(summary).length,
    descriptionIssues: [],
    h1Count: 0,
    h1Issues: [],
    totalImages: 0,
    imagesWithoutAlt: 0,
    readabilityScore: 0
  }

  if (r.titleLength < config.min_title_length) r.titleIssues.push('标题过短（' + r.titleLength + ' 字符），建议至少 ' + config.min_title_length + ' 字符')
  if (r.titleLength > config.max_title_length) r.titleIssues.push('标题过长（' + r.titleLength + ' 字符），建议不超过 ' + config.max_title_length + ' 字符')

  if (r.descriptionLength < config.min_description_length) r.descriptionIssues.push('描述过短（' + r.descriptionLength + ' 字符），建议至少 ' + config.min_description_length + ' 字符')
  if (r.descriptionLength > config.max_description_length) r.descriptionIssues.push('描述过长（' + r.descriptionLength + ' 字符），建议不超过 ' + config.max_description_length + ' 字符')

  const h1s = content.match(/<h1\b[^>]*>[\s\S]*?<\/h1\s*>/gi) || []
  r.h1Count = h1s.length
  if (!r.h1Count) r.h1Issues.push('正文缺少 H1 标题标签')
  else if (r.h1Count > 1) r.h1Issues.push('H1 标签过多（' + r.h1Count + ' 个），建议只有一个')

  const imgs = content.match(/<img[^>]*>/gi) || []
  r.totalImages = imgs.length
  for (const img of imgs) {
    if (!/alt\s*=\s*["'][^"']*["']/i.test(img)) r.imagesWithoutAlt++
  }

  const text = stripTags(content)
  const sentences = (text.match(/[。.!！?？]/g) || []).length || 1
  const avg = Array.from(text).length / sentences
  r.readabilityScore = avg > 0 ? Math.max(0, Math.round(100 - avg / 5)) : 0

  let score = 100
  if (r.titleIssues.length) score -= 15
  if (r.descriptionIssues.length) score -= 15
  if (r.h1Issues.length) score -= 10
  if (r.imagesWithoutAlt) score -= Math.min(20, r.imagesWithoutAlt * 5)
  if (r.readabilityScore < 50) score -= 10
  r.score = Math.max(0, score)
  return r
}

function makeCheck(ok, title, issues, desc) {
  const row = document.createElement('div')
  row.className = 'check'

  const icon = document.createElement('div')
  icon.className = 'check-icon ' + (ok ? 'ok' : 'warn')
  icon.textContent = ok ? '✓' : '!'

  const body = document.createElement('div')
  body.className = 'check-body'

  const t = document.createElement('div')
  t.className = 'check-title'
  t.textContent = title
  body.appendChild(t)

  if (issues && issues.length) {
    const ul = document.createElement('ul')
    ul.className = 'check-issues'
    for (const issue of issues) {
      const li = document.createElement('li')
      li.textContent = issue
      ul.appendChild(li)
    }
    body.appendChild(ul)
  } else if (desc) {
    const p = document.createElement('div')
    p.className = 'muted'
    p.textContent = desc
    body.appendChild(p)
  }

  row.appendChild(icon)
  row.appendChild(body)
  return row
}

function renderLocal() {
  const r = analyze()
  lastResult = r

  const color = r.score >= 80 ? 'var(--ok)' : r.score >= 60 ? 'var(--warn)' : 'var(--danger)'
  $('scoreCircle').style.borderColor = color
  $('scoreValue').style.color = color
  $('scoreValue').textContent = r.score
  $('scoreTip').textContent = (!r.titleIssues.length && !r.descriptionIssues.length && !r.h1Issues.length && !r.imagesWithoutAlt && r.readabilityScore >= 60)
    ? '本地检查全部通过' : '还有优化空间'

  const box = $('checks')
  box.textContent = ''
  box.appendChild(makeCheck(!r.titleIssues.length, '标题（' + r.titleLength + ' 字符）', r.titleIssues))
  box.appendChild(makeCheck(!r.descriptionIssues.length, '描述（' + r.descriptionLength + ' 字符）', r.descriptionIssues))
  box.appendChild(makeCheck(!r.h1Issues.length, 'H1 标签（' + r.h1Count + ' 个）', r.h1Issues))
  box.appendChild(makeCheck(!r.imagesWithoutAlt, '图片 Alt（共 ' + r.totalImages + ' 张）', r.imagesWithoutAlt ? ['有 ' + r.imagesWithoutAlt + ' 张图片缺少 alt 属性'] : []))
  box.appendChild(makeCheck(r.readabilityScore >= 60, '可读性评分（' + r.readabilityScore + '/100）', r.readabilityScore < 60 ? ['句子偏长，建议拆分'] : []))
}

// ---------- AI 分析 ----------
function parseJSON(text) {
  let s = (text || '').trim().replace(/^```(?:json)?/i, '').replace(/```$/, '').trim()
  const start = s.indexOf('{')
  const end = s.lastIndexOf('}')
  if (start >= 0 && end > start) s = s.slice(start, end + 1)
  return JSON.parse(s)
}

function strings(value) {
  return Array.isArray(value) ? value.filter(v => typeof v === 'string' && v.trim()) : []
}

function addSuggestion(parent, text, field) {
  const row = document.createElement('div')
  row.className = 'suggest'
  const p = document.createElement('p')
  p.textContent = text
  const btn = document.createElement('button')
  btn.textContent = '应用'
  btn.addEventListener('click', () => {
    if (appliedState[field] && appliedState[field].row === row) undoField(field)
    else applyField(field, text, row, btn)
  })
  row.appendChild(p)
  row.appendChild(btn)
  parent.appendChild(row)
}

function fieldValue(field) {
  return field === 'title' ? (editorState.title || '') : (editorState.summary || '')
}

function resetRow(state) {
  if (!state) return
  state.row.classList.remove('applied')
  state.btn.textContent = '应用'
}

// 应用一条建议:记录原值,按钮变为「撤销」,可继续应用另一字段的建议
function applyField(field, value, row, btn) {
  const prev = appliedState[field]
  const base = prev ? prev.base : fieldValue(field)
  if (prev) resetRow(prev)
  appliedState[field] = { field, value, base, row, btn }
  row.classList.add('applied')
  btn.textContent = '撤销'
  applyToEditor({ [field]: value })
  // 本地先同步,避免宿主回推前的重复点击取到旧值
  if (field === 'title') editorState.title = value
  else editorState.summary = value
  renderAppliedBar()
}

function undoField(field) {
  const state = appliedState[field]
  if (!state) return
  applyToEditor({ [field]: state.base }, true)
  if (field === 'title') editorState.title = state.base
  else editorState.summary = state.base
  resetRow(state)
  appliedState[field] = null
  renderAppliedBar()
}

function resetApplied() {
  for (const field of ['title', 'summary']) {
    if (appliedState[field]) resetRow(appliedState[field])
    appliedState[field] = null
  }
  renderAppliedBar()
}

function renderAppliedBar() {
  const bar = $('aiApplied')
  if (!bar) return
  bar.textContent = ''
  const active = ['title', 'summary'].filter((f) => appliedState[f])
  if (!active.length) {
    bar.classList.add('hidden')
    return
  }
  bar.classList.remove('hidden')
  const text = document.createElement('span')
  text.textContent = '已应用：' + active.map((f) => (f === 'title' ? '标题' : '摘要')).join('、') + '（可继续应用其它建议）'
  bar.appendChild(text)
  const undoAll = document.createElement('button')
  undoAll.textContent = '全部撤销'
  undoAll.addEventListener('click', () => {
    for (const field of active) undoField(field)
  })
  bar.appendChild(undoAll)
}

// ---------- AI 润色:选中段落的候选 + 差异对比 + 撤销 ----------
function updatePolishHint() {
  const hint = $('polishHint')
  if (!hint) return
  const text = (editorState.selection || '').trim()
  hint.textContent = text
    ? '已选中 ' + text.length + ' 字：' + text.slice(0, 40) + (text.length > 40 ? '…' : '') + '（点「润色选中内容」生成候选）'
    : '在正文里选中一段文字，再点「润色选中内容」；结果以差异对比展示，确认后才替换。'
}

// 粗粒度差异:公共前后缀之外的部分分别标删除/新增
function coarseDiffHtml(oldText, newText) {
  let i = 0
  while (i < oldText.length && i < newText.length && oldText.charAt(i) === newText.charAt(i)) i++
  let j = 0
  while (j < oldText.length - i && j < newText.length - i &&
    oldText.charAt(oldText.length - 1 - j) === newText.charAt(newText.length - 1 - j)) j++
  const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  const prefix = oldText.slice(0, i)
  const oldMid = oldText.slice(i, oldText.length - j)
  const newMid = newText.slice(i, newText.length - j)
  const suffix = oldText.slice(oldText.length - j)
  let html = esc(prefix)
  if (oldMid) html += '<del>' + esc(oldMid) + '</del>'
  if (newMid) html += '<ins>' + esc(newMid) + '</ins>'
  html += esc(suffix)
  return html
}

function resetPolishRow(row, btn) {
  if (row) row.classList.remove('applied')
  if (btn) btn.textContent = '应用'
}

function applyPolish(text, row, btn) {
  if (polishApplied) resetPolishRow(polishApplied.row, polishApplied.btn)
  polishApplied = { row, btn }
  row.classList.add('applied')
  btn.textContent = '撤销'
  applyToEditor({ content_text: text })
}

function undoPolish() {
  if (!polishApplied) return
  window.parent.postMessage({ type: 'fanke:editor:undo' }, location.origin)
  resetPolishRow(polishApplied.row, polishApplied.btn)
  polishApplied = null
}

function renderPolish(cands) {
  const box = $('polishResult')
  box.textContent = ''
  if (polishApplied) {
    resetPolishRow(polishApplied.row, polishApplied.btn)
    polishApplied = null
  }
  if (!cands.length) {
    const p = document.createElement('p')
    p.className = 'empty-tip'
    p.textContent = 'AI 未返回可用候选，请重试。'
    box.appendChild(p)
    return
  }
  cands.forEach((text) => {
    const item = document.createElement('div')
    item.className = 'polish-item'
    const diff = document.createElement('div')
    diff.className = 'polish-diff'
    diff.innerHTML = coarseDiffHtml(polishSource, text)
    const actions = document.createElement('div')
    actions.className = 'polish-actions'
    const btn = document.createElement('button')
    btn.textContent = '应用'
    btn.addEventListener('click', () => {
      if (polishApplied && polishApplied.row === item) undoPolish()
      else applyPolish(text, item, btn)
    })
    const tip = document.createElement('span')
    tip.className = 'muted'
    tip.textContent = '绿色为新增，红色为删除'
    actions.appendChild(btn)
    actions.appendChild(tip)
    item.appendChild(diff)
    item.appendChild(actions)
    box.appendChild(item)
  })
}

async function checkPolish() {
  if (polishBusy) return
  const src = (editorState.selection || '').trim()
  if (!src) {
    notify('请先在正文里选中要润色的段落', false)
    return
  }
  if (src.length > 4000) {
    notify('一次最多润色 4000 字，请分段选择', false)
    return
  }
  polishBusy = true
  polishSource = src
  const btn = $('polishBtn')
  btn.disabled = true
  btn.textContent = '润色中…'
  try {
    const prompt = [
      '你是资深中文编辑。请对下面这段文字做轻度润色：保留原意与事实，不增删信息，修正病句与口语，统一标点，保持原有换行。',
      '不要输出 Markdown 或 HTML 标签，只返回纯文本。',
      '请给出两个不同侧重的候选，只返回 JSON：{"candidates":["候选1","候选2"]}',
      '原文：',
      src
    ].join('\n')
    const res = await Fanke.callHook('ai.chat', {
      messages: [{ role: 'user', content: prompt }],
      temperature: 0.5,
      max_tokens: 1600
    })
    const data = parseJSON(res && res.content)
    renderPolish(strings(data && data.candidates))
    notify('润色完成，请对比后应用', true)
  } catch (e) {
    void notifyUser('error', 'AI 润色失败', String(e.message || e), 'modal')
  } finally {
    polishBusy = false
    btn.disabled = false
    btn.textContent = '润色选中内容'
  }
}

function sectionTitle(parent, text) {
  const h = document.createElement('div')
  h.className = 'check-title'
  h.style.margin = '0.75rem 0 0.375rem'
  h.textContent = text
  parent.appendChild(h)
}

function renderAI(data) {
  const box = $('aiResult')
  box.textContent = ''
  resetApplied()
  if (!data) return

  const titles = strings(data.title_suggestions)
  if (titles.length) {
    sectionTitle(box, '标题建议')
    for (const t of titles) addSuggestion(box, t, 'title')
  }

  const descs = strings(data.description_suggestions)
  if (descs.length) {
    sectionTitle(box, '描述建议')
    for (const d of descs) addSuggestion(box, d, 'summary')
  }

  const keywords = strings(data.keywords)
  if (keywords.length) {
    sectionTitle(box, '关键词')
    const wrap = document.createElement('div')
    wrap.className = 'keywords'
    for (const k of keywords) {
      const tag = document.createElement('span')
      tag.className = 'keyword'
      tag.textContent = k
      wrap.appendChild(tag)
    }
    box.appendChild(wrap)
  }

  const contents = strings(data.content_suggestions)
  if (contents.length) {
    sectionTitle(box, '正文建议（仅供参考）')
    for (const c of contents) {
      const p = document.createElement('div')
      p.className = 'muted'
      p.style.marginBottom = '0.375rem'
      p.textContent = '· ' + c
      box.appendChild(p)
    }
  }

  const structure = strings(data.structure_suggestions || data.details && data.details.recommendations)
  if (structure.length) {
    sectionTitle(box, '结构建议')
    for (const s of structure) {
      const p = document.createElement('div')
      p.className = 'muted'
      p.style.marginBottom = '0.375rem'
      p.textContent = '· ' + s
      box.appendChild(p)
    }
  }

  if (!box.childElementCount) {
    const p = document.createElement('p')
    p.className = 'empty-tip'
    p.textContent = 'AI 未返回可用建议。'
    box.appendChild(p)
  }
}

async function checkAI() {
  if (aiBusy) return
  if (!editorState.title && !editorState.content) {
    notify('请先填写标题或正文', false)
    return
  }
  aiBusy = true
  const btn = $('aiBtn')
  btn.disabled = true
  btn.textContent = '分析中…'
  $('aiHint').textContent = '正在等待 AI 返回…'
  try {
    const body = stripTags(editorState.content).slice(0, 12000)
    const prompt = [
      '你是资深中文博客 SEO 编辑。请分析下面的文章并给出可直接采用的建议。',
      '标题：' + (editorState.title || '（未填写）'),
      '摘要：' + (editorState.summary || '（未填写）'),
      '正文：' + body,
      '只返回 JSON，不要任何解释或代码块，结构为：',
      '{"title_suggestions":["..."],"description_suggestions":["..."],"keywords":["..."],"content_suggestions":["..."],"structure_suggestions":["..."]}',
      '要求：标题建议 3 条，摘要建议 2 条，关键词 3-8 个，正文与结构建议各 2-4 条；每条不超过 80 字。'
    ].join('\n')
    const res = await Fanke.callHook('ai.chat', {
      messages: [{ role: 'user', content: prompt }],
      temperature: 0.4,
      max_tokens: 1600
    })
    const data = parseJSON(res && res.content)
    renderAI(data)
    $('aiHint').textContent = 'AI 分析完成' + (res && res.model ? '（' + res.model + '）' : '') + '，请核对后应用。'
    notify('AI 分析完成', true)
  } catch (e) {
    const msg = String(e.message || e)
    $('aiHint').textContent = 'AI 分析失败：' + msg
    // 弹出通知里会带上后端聚合的每条连接失败原因(分组/模型 + 上游报错)
    void notifyUser('error', 'AI 调用失败', msg, 'modal')
  } finally {
    aiBusy = false
    btn.disabled = false
    btn.textContent = '重新分析'
  }
}

function applyToEditor(payload, allowEmpty) {
  if (!isPanel) return
  const body = Object.assign({}, payload)
  if (allowEmpty) body.allow_empty = true
  window.parent.postMessage({ type: 'fanke:editor:apply', payload: body }, location.origin)
}

// ---------- 消息通道 ----------
function onMessage(ev) {
  if (ev.origin !== location.origin) return
  const d = ev.data
  if (!d || d.type !== 'fanke:editor:content' || !d.payload) return
  editorState = {
    title: String(d.payload.title || ''),
    summary: String(d.payload.summary || ''),
    content: String(d.payload.content || ''),
    selection: String(d.payload.selection || ''),
    post_id: d.payload.post_id || null
  }
  if (isPanel) {
    renderLocal()
    updatePolishHint()
    // 内容变化(包括应用建议后宿主回推)不再清空 AI 结果,
    // 否则点一条建议就会"整页重置",无法继续应用其它建议或撤销。
    const hasAI = $('aiResult').childElementCount > 0
    if (!hasAI && !appliedState.title && !appliedState.summary) {
      $('aiHint').textContent = '基于当前编辑器内容，由 AI 连接按后台设置的分组/模型执行。'
      $('aiBtn').textContent = '开始分析'
    }
  }
}

// ---------- 启动 ----------
function boot() {
  if (!window.Fanke) {
    showToast('Fanke 环境未就绪', false)
    return
  }
  if (isPanel) {
    document.body.classList.add('panel-mode')
    $('settingsView').classList.add('hidden')
    $('panelView').classList.remove('hidden')
    window.addEventListener('message', onMessage)
    $('aiBtn').addEventListener('click', checkAI)
    $('polishBtn').addEventListener('click', checkPolish)
    updatePolishHint()
    loadConfig()
    return
  }
  updateSettingsUI()
  $('saveBtn').addEventListener('click', () => saveConfig().catch(e => notify('保存失败: ' + (e.message || e), false)))
  $('resetBtn').addEventListener('click', async () => {
    config = { ...defaultConfig }
    updateSettingsUI()
    try { await saveConfig() } catch (e) { notify('保存失败: ' + (e.message || e), false) }
  })
  if (Fanke.isInit) {
    $('saveBtn').disabled = true
    $('resetBtn').disabled = true
  } else {
    loadConfig().then(updateSettingsUI)
  }
}

if (window.Fanke) {
  boot()
} else {
  document.addEventListener('DOMContentLoaded', boot)
}
