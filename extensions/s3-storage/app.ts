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

// s3-storage 插件脚本（TypeScript 源文件，安装时自动编译为 app.js）
// 逻辑与旧版 init/index.html 内联脚本保持一致，仅做类型化整理。

(() => {
  const sameOrigin = location.origin && location.origin !== 'null' ? location.origin : '';
  const targetOrigin = sameOrigin || '*';
  const pending = new Map();
  const confirmPending = new Map();
  let seq = 0;
  window.addEventListener('message', (event) => {
    if (event.source !== parent) return;
    if (sameOrigin && event.origin !== sameOrigin) return;
    const data = event.data;
    if (data && data.type === 'fanke:confirm:result' && confirmPending.has(data.id)) {
      const done = confirmPending.get(data.id); confirmPending.delete(data.id);
      done(!!data.confirmed);
      return;
    }
    if (!data || data.type !== 'fanke:plugin-config:result' || !pending.has(data.id)) return;
    const item = pending.get(data.id); pending.delete(data.id); clearTimeout(item.timer);
    data.error ? item.reject(new Error(data.error)) : item.resolve(data.result);
  });
  function askConfirm(message) {
    if (parent === window) return Promise.resolve(confirm(message));
    return new Promise((resolve) => {
      const id = `s3-confirm-${Date.now()}-${++seq}`;
      confirmPending.set(id, resolve);
      parent.postMessage({ type: 'fanke:confirm', id, message }, targetOrigin);
    });
  }
  function call(action, payload) {
    return new Promise((resolve, reject) => {
      const id = `s3-${Date.now()}-${++seq}`;
      const timer = setTimeout(() => { pending.delete(id); reject(new Error('请求超时，请在后台插件详情页重试')); }, 30000);
      pending.set(id, { resolve, reject, timer });
      parent.postMessage({ type: 'fanke:plugin-config', id, action, payload }, targetOrigin);
    });
  }
  const container = document.getElementById('buckets');
  const editor = document.getElementById('editor');
  let buckets = [], defaultId = '', working = false;
  const fields = [['alias', '上传菜单别名'], ['endpoint', 'Endpoint', 'https://s3.example.com'], ['region', '区域 Region', 'auto'], ['bucket', '存储桶名称'], ['public_base', '公开访问地址', 'https://cdn.example.com'], ['prefix', '对象路径前缀', 'blog/'], ['access_key', 'Access Key ID'], ['secret_key', 'Secret Access Key'], ['session_token', 'Session Token（可选）']];
  const secrets = new Set(['access_key', 'secret_key', 'session_token']);
  function status(message, error = false) { const el = document.getElementById('status'); el.textContent = message; el.className = 'status' + (error ? ' error' : ''); }
  function render() {
    container.replaceChildren();
    buckets.forEach((bucket, index) => {
      const section = document.createElement('section'); section.className = 'card';
      const header = document.createElement('div'); header.className = 'row toolbar';
      const title = document.createElement('h2'); title.textContent = `存储桶 ${index + 1}`;
      const remove = document.createElement('button'); remove.type = 'button'; remove.className = 'danger'; remove.textContent = '移除';
      remove.onclick = async () => { if (!(await askConfirm('移除此桶配置？保存全部配置后生效，不会删除远端对象。'))) return; const idx = buckets.indexOf(bucket); if (idx >= 0) buckets.splice(idx, 1); if (defaultId === bucket.id) defaultId = ''; render(); };
      header.append(title, remove); section.append(header);
      const toggles = document.createElement('div'); toggles.className = 'row';
      const enabledLabel = document.createElement('label'), enabled = document.createElement('input'); enabled.type = 'checkbox'; enabled.checked = bucket.enabled; enabled.onchange = () => { bucket.enabled = enabled.checked; if (!bucket.enabled && defaultId === bucket.id) defaultId = ''; render(); }; enabledLabel.append(enabled, ' 启用此桶');
      const defaultLabel = document.createElement('label'), radio = document.createElement('input'); radio.type = 'radio'; radio.name = 'default'; radio.checked = defaultId === bucket.id; radio.disabled = !bucket.enabled; radio.onchange = () => { defaultId = bucket.id; }; defaultLabel.append(radio, ' 默认 S3 桶'); toggles.append(enabledLabel, defaultLabel); section.append(toggles);
      const grid = document.createElement('div'); grid.className = 'grid';
      fields.forEach(([key, label, placeholder]) => {
        const wrapper = document.createElement('label'); wrapper.textContent = label;
        const input = document.createElement('input'); input.type = secrets.has(key) ? 'password' : 'text'; input.autocomplete = secrets.has(key) ? 'new-password' : 'off';
        input.value = bucket[key] || ''; input.placeholder = secrets.has(key) && bucket.savedKeys.has(key) ? '已保存，留空保留' : placeholder || '';
        input.oninput = () => { bucket[key] = input.value; };
        if (key === 'bucket') { input.setAttribute('list', `bucket-list-${index}`); const list = document.createElement('datalist'); list.id = `bucket-list-${index}`; wrapper.append(list); }
        wrapper.append(input); grid.append(wrapper);
      });
      section.append(grid);
      const pathLabel = document.createElement('label'), path = document.createElement('input'); path.type = 'checkbox'; path.checked = bucket.force_path_style; path.onchange = () => { bucket.force_path_style = path.checked; }; pathLabel.append(path, ' 使用路径式访问（force_path_style）'); section.append(pathLabel);
      const actions = document.createElement('div'); actions.className = 'row'; actions.style.marginTop = '16px';
      ['buckets', 'test'].forEach((action) => {
        const button = document.createElement('button'); button.type = 'button'; button.textContent = action === 'buckets' ? '获取桶列表' : '测试读写';
        button.onclick = () => operation(async () => {
          const result = await call(action, { ...serialize(bucket), bucket_id: bucket.id });
          if (action === 'buckets') {
            const names = Array.isArray(result?.buckets) ? result.buckets : [];
            const list = section.querySelector('datalist'); list.replaceChildren(); names.forEach((name) => { const option = document.createElement('option'); option.value = String(name); list.append(option); });
            status(`已找到 ${names.length} 个桶，请在存储桶名称输入框选择。`);
          } else status('读写测试通过。');
        }); actions.append(button);
      }); section.append(actions); container.append(section);
    });
    if (!buckets.length) { const empty = document.createElement('p'); empty.className = 'card muted'; empty.textContent = '尚未配置存储桶，点击下方添加。'; container.append(empty); }
  }
  function serialize(bucket) {
    const result = { id: bucket.id, enabled: bucket.enabled, force_path_style: bucket.force_path_style };
    fields.forEach(([key]) => { const value = (bucket[key] || '').trim(); result[key] = secrets.has(key) && /^\*+$/.test(value) ? '' : value; });
    return result;
  }
  async function operation(fn) {
    if (working) return;
    working = true; editor.disabled = true; status('正在处理…');
    try { await fn(); } catch (error) { status(error.message || '操作失败', true); } finally { working = false; editor.disabled = false; }
  }
  async function load() {
    editor.disabled = true; document.getElementById('retry').hidden = true;
    try {
      const config = await call('get');
      buckets = (config.buckets || []).map((bucket) => {
        const savedKeys = new Set(); const item = { ...bucket, savedKeys, force_path_style: bucket.force_path_style !== false };
        secrets.forEach((key) => { if (item[key]) savedKeys.add(key); item[key] = ''; });
        return item;
      });
      defaultId = config.default_bucket_id || ''; render(); editor.disabled = false; status('配置已读取。密钥不会回显；留空可保留。');
    } catch (error) { status(error.message, true); document.getElementById('retry').hidden = false; throw error; }
  }
  document.getElementById('add').onclick = () => { buckets.push({ id: crypto.randomUUID(), alias: '', enabled: true, region: 'auto', force_path_style: true, savedKeys: new Set() }); render(); };
  document.getElementById('reload').onclick = async () => { if (await askConfirm('重新读取将丢弃未保存修改，继续？')) load().catch(() => {}); };
  document.getElementById('retry').onclick = () => load().catch(() => {});
  document.getElementById('config').onsubmit = (event) => {
    event.preventDefault(); operation(async () => {
      if (buckets.some((bucket) => bucket.enabled && (!bucket.alias?.trim() || !bucket.endpoint?.trim() || !bucket.bucket?.trim() || !bucket.public_base?.trim()))) throw new Error('请补全已启用桶的别名、Endpoint、桶名称和公开访问地址。');
      if (buckets.some((bucket) => bucket.enabled) && !buckets.some((bucket) => bucket.enabled && bucket.id === defaultId)) throw new Error('请为已启用的桶选择默认 S3 桶。');
      await call('put', { buckets: buckets.map(serialize), default_bucket_id: defaultId });
      await load(); status('全部配置已保存，上传菜单已刷新。');
    });
  };
  load().catch(() => {});
})();
