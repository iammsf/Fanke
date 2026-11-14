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

// image-processing 插件脚本（TypeScript 源文件，安装时自动编译为 app.js）
// 逻辑与旧版 init/index.html 内联脚本保持一致，仅做类型化整理。

(() => {
  const origin = location.origin, pending = new Map(); let seq = 0, working = false;
  window.addEventListener('message', (event) => {
    if (event.origin !== origin || event.source !== parent) return;
    const data = event.data;
    if (!data || data.type !== 'fanke:plugin-config:result' || !pending.has(data.id)) return;
    const item = pending.get(data.id); pending.delete(data.id); clearTimeout(item.timer);
    data.error ? item.reject(new Error(data.error)) : item.resolve(data.result);
  });
  function call(action, payload) {
    return new Promise((resolve, reject) => {
      const id = `image-${Date.now()}-${++seq}`;
      const timer = setTimeout(() => { pending.delete(id); reject(new Error('请求超时，请在后台插件详情页重试')); }, 30000);
      pending.set(id, { resolve, reject, timer });
      parent.postMessage({ type: 'fanke:plugin-config', id, action, payload }, origin);
    });
  }
  const editor = document.getElementById('editor');
  function status(message, error = false) { const el = document.getElementById('status'); el.textContent = message; el.className = 'status' + (error ? ' error' : ''); }
  async function load() {
    editor.disabled = true; document.getElementById('retry').hidden = true;
    try {
      const result = await call('get'); document.getElementById('enabled').checked = result.enabled === true;
      document.getElementById('widths').value = (result.widths || [480, 740, 1080]).join(', ');
      document.getElementById('quality').value = result.quality ?? 82;
      editor.disabled = false; status('配置已读取。');
    } catch (error) { status(error.message, true); document.getElementById('retry').hidden = false; }
  }
  document.getElementById('config').onsubmit = async (event) => {
    event.preventDefault(); if (working) return;
    const raw = document.getElementById('widths').value.trim().split(/[,，\s]+/);
    const widths = [...new Set(raw.map(Number))].sort((a, b) => a - b);
    const quality = Number(document.getElementById('quality').value);
    if (!widths.length || widths.some((width) => !Number.isSafeInteger(width) || width <= 0)) { status('宽度必须为以逗号分隔的正整数。', true); return; }
    if (!Number.isInteger(quality) || quality < 1 || quality > 100) { status('质量必须为 1–100 的整数。', true); return; }
    working = true; editor.disabled = true; status('正在保存…');
    try {
      await call('put', { enabled: document.getElementById('enabled').checked, widths, quality });
      document.getElementById('widths').value = widths.join(', '); status('图片处理设置已保存。');
    } catch (error) { status(error.message || '保存失败', true); } finally { working = false; editor.disabled = false; }
  };
  document.getElementById('reload').onclick = () => { if (confirm('重新读取将丢弃未保存修改，继续？')) load(); };
  document.getElementById('retry').onclick = load;
  load();
})();
