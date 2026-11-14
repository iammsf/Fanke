# 插件钩子函数参考

> 最后更新：2026-11-14
> 本文以 `backend/internal/app` 下的 hook 注册代码为准（`hooks.go`、`hooks_store.go`、`hooks_ai.go`、`hooks_notify.go`、`lifecycle.go`、`media_plugins.go`、`handlers_extensions.go`、`middleware.go`）。
> 插件包格式、安装流程、`window.Fanke` 桥接 API 的完整说明见 `doc/plugins.md`；两篇文档的 hook 部分必须保持一致。

本文覆盖后端目前注册的 **28 个 hook**：14 个能力 API、10 个生命周期事件、4 个声明式扩展点。

---

## 一、维护规则

1. **改 hook 必须改文档**：新增、修改、删除任何一个 hook（能力 API / 生命周期事件 / 声明式扩展点），必须同时更新本文档与 `doc/plugins.md`。本文档至少同步三处：速查表、对应详解章节、附录（限流表 / 错误对照）。
2. **插件必须先声明再调用**：插件使用任何 hook，必须在 `info.json` 的 `request` 数组中写明确名字，否则调用会被拒绝，错误为 `插件未声明该 hook: <name>`。安装时 `ValidateHooks` 也会拦截不存在的声明（422 + `missing_hooks`）。
3. **新增 hook 的落地顺序**（后端侧）：
   - 用 `RegisterHook(&Hook{Name, Description, Public, Fn})` 注册。`Name` 与 `Fn` 必填；不得与 `lifecycleEvents` 重名（会 panic）；不得重复注册同名 hook（会 panic）。
   - 在 `middleware.go` 的 `hookRateLimits` 中为该 hook 设定每分钟上限；不设置则使用默认 60 次/分钟。
   - 谨慎设置 `Public: true`：公开意味着匿名访客可调用，必须确认返回数据可以公开。
   - 补参数校验、数量上限与错误文案，再更新本文档与 `doc/plugins.md`。
4. **插件需要新能力时**：正确做法是后端新增 hook（或新增 Go 生命周期处理器）+ 补文档 + 插件在 `request` 声明；不允许插件绕过桥接直接请求内部接口，也不允许把业务逻辑塞进 `init.js` 伪装成后端能力。
5. **payload / 返回结构属于契约**：字段改名、类型变化、上限收紧都算破坏性变更，必须在文档中注明，并评估对已安装旧插件的影响。

---

## 二、速查表

| 名称 | 方向 | 是否公开 | 每分钟限流 | 一句话说明 |
| --- | --- | --- | --- | --- |
| `store.ensure_table` | 能力 API | 否 | 5 | 创建本插件 KV 表（幂等） |
| `store.save` | 能力 API | 否 | 120 | 按 key upsert 一条记录 |
| `store.get` | 能力 API | 否 | 300 | 按 key 读取一条记录 |
| `store.get_batch` | 能力 API | **是** | 120 | 批量按 key 读取（前台免登录可用） |
| `store.list` | 能力 API | 否 | 60 | 列出本插件全部记录 |
| `store.import` | 能力 API | 否 | 12 | 批量导入，可选不覆盖已有 |
| `store.export` | 能力 API | 否 | 20 | 导出本插件全部记录 |
| `store.dedup` | 能力 API | 否 | 10 | 删除重复 key（兜底） |
| `store.delete` | 能力 API | 否 | 60 | 按 key 列表删除 |
| `store.stats` | 能力 API | 否 | 120 | 返回记录总数 |
| `media.list` | 能力 API | 否 | 60 | 列出媒体库文件，可按 mime 前缀过滤 |
| `http.check_url` | 能力 API | 否 | 30 | 服务端探测一组 url 的可达性 |
| `ai.chat` | 能力 API | 否 | 20 | 调用后台配置的 AI 连接对话 |
| `notify.send` | 能力 API | 否 | 60 | 请求宿主显示通知 |
| `beforePostCreate` | 生命周期事件 | — | 不适用 | 文章创建保存前 |
| `afterPostCreated` | 生命周期事件 | — | 不适用 | 文章创建成功后 |
| `beforePostUpdate` | 生命周期事件 | — | 不适用 | 文章正文更新保存前 |
| `afterPostUpdated` | 生命周期事件 | — | 不适用 | 文章正文更新成功后 |
| `beforePostDelete` | 生命周期事件 | — | 不适用 | 文章删除前 |
| `afterPostDeleted` | 生命周期事件 | — | 不适用 | 文章删除成功后 |
| `beforeMediaUpload` | 生命周期事件 | — | 不适用 | 媒体上传（含去重复用分支）前 |
| `afterMediaUploaded` | 生命周期事件 | — | 不适用 | 媒体上传成功（含复用）后 |
| `beforeMediaDelete` | 生命周期事件 | — | 不适用 | 媒体删除前（含批量、同步清理） |
| `afterMediaDeleted` | 生命周期事件 | — | 不适用 | 媒体删除成功后 |
| `addSettingsItems` | 声明式扩展点 | — | 不适用 | 在后台设置页挂插件配置入口 |
| `addUploadMediaMenuItem` | 声明式扩展点 | — | 不适用 | 在媒体上传菜单挂存储选项 |
| `addEditorPanel` | 声明式扩展点 | — | 不适用 | 在文章编辑器挂插件面板（iframe） |
| `editor.register` | 声明式扩展点 | — | 不适用 | 让后台编辑器加载插件 `editor.js` 注册自定义标签 |

> “公开”只对能力 API 有意义：公开 hook 可由匿名请求经 `POST /api/v1/ext/{slug}/hook/{name}` 调用，且要求插件已启用。生命周期事件与声明式扩展点不经过 hook 调用通道，没有限流值。

---

## 三、通用约定

### 3.1 调用方式

插件包内的 JavaScript 统一通过桥接对象调用：

```js
// 返回 Promise；resolve 为 hook 的返回值（即后端的 data 字段），reject 为 Error(message)
const data = await Fanke.callHook('store.get', { key: 'config' })
```

桥接实现（后端注入到插件 `index.html`）：iframe 内 `parent.postMessage({ type: 'fanke:hook', id, hook, payload })`，父页面带管理员凭证请求管理端接口，再把 `{ type: 'fanke:hook:result', id, ok, data, error }` 回传。插件 iframe 不持有登录凭证，也不直接访问后端接口。

实际 HTTP 通道有三点需要注意：

| 通道 | 方法 / 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| 管理端（桥接默认） | `POST /api/v1/admin/extensions/{slug}/hook/{hook}` | 管理员 Bearer | 不看插件启用状态，详情页初始化阶段也能调用；同一 `(slug, hook)` 与公开通道共用限流桶 |
| 公开 | `POST /api/v1/ext/{slug}/hook/{hook}` | 匿名 | 仅 `Public: true` 的 hook；要求插件已启用；该路由另有 chi `Throttle(60)` 的**并发**上限（不是每分钟配额） |
| 生命周期 / 声明式 | 无 HTTP 入口 | — | 只能由后端业务触发 / 宿主前端加载，不能通过 HTTP 或 `callHook` 调用 |

payload 读取上限为 4 MiB（`io.LimitReader(r.Body, 4<<20)`）。请求体为空时 payload 为 `nil`，而不是 `{}`——对必需参数的 hook 会直接报解析错误。

### 3.2 返回结构

HTTP 层统一为：

```json
{ "ok": true, "data": { } }
```

`Fanke.callHook` 已经把外层剥掉，JavaScript 拿到的就是 `data` 本身。下文各 hook 的“返回”均指 `data`。

### 3.3 错误处理

- **所有 hook 执行错误（参数校验失败、业务失败、数据库错误）统一返回 HTTP 400**，响应体为 `{ "error": "<错误文案>" }`。桥接会 reject，`Error.message` 即该文案，可直接展示给用户。
- 通道级错误：`404` 插件不存在、`409` 插件未启用（仅公开通道）、`429` 超出限流（带 `Retry-After: 60`）。
- 错误文案来自 Go 实现，属于对外契约；修改文案时需同步本文档附录。

```js
try {
  await Fanke.callHook('store.save', { key: 'config', value: cfg })
} catch (e) {
  Fanke.emit('toast', { message: '保存失败：' + e.message, ok: false })
}
```

### 3.4 声明与校验

- 安装插件时，`info.json` 的 `request` 会交给 `ValidateHooks` 校验：名字必须存在于能力 hook 注册表、`lifecycleEvents` 或 `declarativeEvents` 之一，否则整个安装被阻止：

  ```json
  {
    "error": "插件声明的 hook 在当前后端不可用,已阻止安装",
    "missing_hooks": ["store.foo"]
  }
  ```

- 调用时 `InvokeHook` 的检查顺序固定为：
  1. 名字是否是生命周期事件 → 是则拒绝（`生命周期事件不可手动调用: <name>`）；
  2. hook 是否存在 → 不存在则拒绝（`hook 不存在: <name>`；声明式扩展点也走这条，因为不在能力注册表里）；
  3. 是否公开通道调用但 hook 非公开 → 拒绝（`hook 不开放公开调用: <name>`）；
  4. 插件 `hooks`（即 `request`）中是否声明 → 未声明则拒绝（`插件未声明该 hook: <name>`）；
  5. 执行 `Fn`。
- **声明的双重含义**：
  - 能力 API：声明 = 获得调用权限。
  - 生命周期事件：声明 = 订阅事件；但只有后端通过 `RegisterLifecycleHandler` 注册了该 slug + 事件的 Go 处理器，且插件已启用、已初始化，事件才会真正分发到处理器。仅有声明不会产生任何逻辑。
  - 声明式扩展点：声明 = 让宿主把该插件纳入对应能力清单（详见第六节）。
- `HookNames()`（管理端插件列表返回的 `hooks` 字段）会同时列出三类名称，仅用于展示，不代表全部可调用。

### 3.5 限流与 Retry-After

- 限流维度是 **`(插件 slug, hook 名)`**，令牌桶：速率 `每分钟上限 / 60` 每秒，桶容量等于每分钟上限（允许一分钟内突发到上限）。
- 管理端通道与公开通道**共用同一个桶**，不能通过切换通道绕过限制。
- 超出后返回 `429`，响应头 `Retry-After: 60`，文案形如：

  ```
  hook 调用过于频繁: ai.chat(每个插件上限 20 次/分钟)
  ```

- 限流为进程内内存实现，按 slug 计数；进程重启归零，多实例部署时各实例独立计数（实际额度约为实例数 × 上限）。
- 生命周期事件与声明式扩展点不经过该限流通道。

---

## 四、能力 API 详解

能力 API 是面向所有插件的通用能力，不绑定具体插件。全部 14 个都在 `hookRegistry` 中注册，默认只能经管理端通道调用（`store.get_batch` 除外）。

### 4.1 存储 `store.*`

存储是**每插件隔离的 KV 表**：

- 表名由 slug 派生：`ext_data_<slug>`，slug 中的 `-` 替换为 `_`，例如 `image-viewer` → `ext_data_image_viewer`。
- 表结构：`id BIGSERIAL PRIMARY KEY`、`k TEXT NOT NULL UNIQUE`、`v JSONB NOT NULL`、`created_at TIMESTAMPTZ`、`updated_at TIMESTAMPTZ`。
- 插件之间互不可见，也接触不到系统表；插件无法执行任意 SQL，只能通过这 10 个 hook 操作自己的表。

| hook | 用途 | 参数上限 |
| --- | --- | --- |
| `store.ensure_table` | 建表 + k 索引（幂等） | 无参数 |
| `store.save` | upsert 单条 | key 1–2048 字节 |
| `store.get` | 读单条 | key 必填 |
| `store.get_batch` | 批量读（公开） | ≤500 个 key |
| `store.list` | 全量列出 | 无分页 |
| `store.import` | 批量导入 | ≤5000 条 |
| `store.export` | 全量导出 | 无参数 |
| `store.dedup` | 去重兜底 | 无参数 |
| `store.delete` | 批量删除 | ≤1000 个 key |
| `store.stats` | 总数统计 | 无参数 |

> ⚠️ `store.get_batch` 是公开 hook：插件一旦启用，其 store 内容对匿名访客可读。**不要往 store 放密钥、凭证、用户隐私等敏感数据。**

#### `store.ensure_table`

**用途**：创建本插件的数据表与 k 索引，幂等。应在 `init.js` / `init.ts` 初始化流程中首先调用；未建表时其他 store hook 会因表不存在而失败（`store.get` 例外，见下）。

**参数**：无。

**返回**：

```json
{ "ok": true, "table": "ext_data_image_viewer" }
```

**可能的错误**：数据库执行错误（建表或建索引失败）会以原始错误文案返回；索引创建失败时 `ok` 为 `false` 且同时返回错误。

**示例**：

```js
// init.js / init.ts
try {
  await Fanke.callHook('store.ensure_table')
  Fanke.initDone()
} catch (e) {
  Fanke.emit('toast', { message: '初始化失败：' + e.message, ok: false })
}
```

**注意**：

- 表名由后端按 slug 计算，插件不能自定义。
- 幂等但有限流（5 次/分钟），不要在页面加载时反复调用。

#### `store.save`

**用途**：保存一条记录；key 已存在则覆盖 `v` 并刷新 `updated_at`。

**参数**：

| 字段 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `key` | string | 是 | — | 长度 1–2048 字节 |
| `value` | 任意 JSON | 是 | — | 必须存在且非空字节；写入 JSONB |

**返回**：

```json
{ "ok": true }
```

**可能的错误**：

- `payload 解析失败,期望 {key, value}`：payload 为空/非 JSON，或缺少 `value`（无字节）。
- `key 非法(长度须 1-2048)`：key 为空或超过 2048 字节。
- 数据库错误原样返回。

**示例**：

```js
await Fanke.callHook('store.save', {
  key: '/uploads/2026/cat.jpg',
  value: { cx: 0.42, cy: 0.3, note: '焦点在猫脸上' }
})
```

**注意**：

- `value` 是 JSON 值，不要传 `undefined`；字符串、数组、数字都可以，但建议统一存对象，便于后续演进。
- key 区分大小写，且是 TEXT 主键；建议用稳定的业务标识（URL、slug、配置名）。

#### `store.get`

**用途**：按 key 读取单条记录。

**参数**：

| 字段 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `key` | string | 是 | — | 非空；不做 2048 长度校验 |

**返回**：

未找到：

```json
{ "found": false }
```

找到：

```json
{
  "found": true,
  "key": "config",
  "value": { "des_pos": "middle bottom" },
  "updated_at": "2026-11-14T08:00:00Z"
}
```

**可能的错误**：`key required`（payload 解析失败或 key 为空）。

**示例**：

```js
const d = await Fanke.callHook('store.get', { key: 'config' })
const cfg = d.found ? d.value : {}
```

**注意**：

- 实现把**任何查询错误都当作未找到**（包括表不存在、连接失败），统一返回 `found: false`。因此表未创建时不会报错，而是永远“没有配置”，容易掩盖初始化问题；先 `store.ensure_table`。
- 单 key 读取上限 300 次/分钟，是 store 中额度最高的，适合前台高频读取单个配置（但仍需公开 hook 才能免登录）。

#### `store.get_batch`（公开）

**用途**：批量按 key 读取，返回 `key → value` 映射。是为前台页面准备的免登录读取能力（例如文章页根据图片 URL 批量取展示配置）。

**参数**：

| 字段 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `keys` | string[] | 是 | — | 一次最多 500 个；传空数组直接返回空结果 |

**返回**：

```json
{
  "items": {
    "/uploads/a.jpg": { "cx": 0.5, "cy": 0.5 },
    "/uploads/b.jpg": { "cx": 0.2, "cy": 0.8 }
  }
}
```

未找到的 key **不会**出现在 `items` 中。

**可能的错误**：

- `payload 解析失败`：payload 为空或不是合法 JSON。
- `一次最多 500 条`：`keys` 超过 500。
- 数据库错误原样返回。

**示例**：

```js
const urls = ['/uploads/a.jpg', '/uploads/b.jpg']
const d = await Fanke.callHook('store.get_batch', { keys: urls })
urls.forEach(u => { if (d.items[u]) applyConfig(u, d.items[u]) })
```

前台直接调用公开通道时：

```js
const res = await fetch('/api/v1/ext/image-focus/hook/store.get_batch', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ keys: ['/uploads/a.jpg'] })
})
const { ok, data } = await res.json()   // data.items
```

**注意**：

- 返回值只有 `value`，不含 `updated_at`；需要时间戳请用 `store.get` / `store.list`。
- 公开通道要求插件**已启用**；未启用返回 409 `插件未启用`。
- 公开 = 匿名可读，再次强调敏感数据不能进 store。
- 重复的 key 在 SQL `ANY` 中自然去重；不存在的 key 被静默忽略。

#### `store.list`

**用途**：按 `id` 升序列出本插件全部记录。

**参数**：无。

**返回**：

```json
{
  "items": [
    { "key": "config", "value": { "a": 1 }, "updated_at": "2026-11-14T08:00:00Z" },
    { "key": "/uploads/a.jpg", "value": { "cx": 0.5 }, "updated_at": "2026-11-14T09:00:00Z" }
  ],
  "total": 2
}
```

**可能的错误**：数据库错误。

**示例**：

```js
const d = await Fanke.callHook('store.list')
console.log(d.total)
```

**注意**：没有分页、没有条数上限。记录量大的插件（例如按图片逐条存储）应改用 `store.get_batch` 或 `store.stats`，避免一次拉全表。

#### `store.import`

**用途**：批量导入记录。适合扫描初始化：把媒体库里还没配置的条目一次性补齐。

**参数**：

| 字段 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `items` | `{key, value}[]` | 是 | — | 一次最多 5000 条 |
| `overwrite` | boolean | 否 | `true` | `true` 覆盖已有 key；`false` 时已有 key 保持不动 |

**返回**：

```json
{ "imported": 128, "skipped": 3 }
```

计入 `skipped` 的情况：key 非法、`value` 为空、写入失败、`overwrite=false` 且 key 已存在。

**可能的错误**：

- `payload 解析失败,期望 {items:[{key,value}...], overwrite?}`
- `一次最多导入 5000 条`

**示例**：

```js
const media = await Fanke.callHook('media.list', { mime_prefix: 'image/' })
const items = media.items.map(m => ({ key: m.url, value: { inited: true } }))
const r = await Fanke.callHook('store.import', { items, overwrite: false })
Fanke.emit('toast', { message: `新增 ${r.imported} 条，跳过 ${r.skipped} 条` })
```

**注意**：

- 非事务实现：逐条 `Exec`，中途失败不会回滚已写入的记录，只在 `skipped` 中计数。需要严格一致的场景请自行分批并重试。
- `overwrite=false` 是扫描初始化的推荐用法，避免覆盖用户已有配置。
- 单条写入失败不会让整个调用失败，因此务必检查 `skipped`。

#### `store.export`

**用途**：导出本插件全部记录，返回可直接喂给 `store.import` 的结构，也便于插件做备份/迁移。

**参数**：无。

**返回**：

```json
{
  "format": "fanke-store/export@1",
  "plugin": "image-focus",
  "count": 131,
  "items": [
    { "key": "config", "value": { "a": 1 }, "updated_at": "2026-11-14T08:00:00Z" }
  ],
  "exported": "2026-11-14T09:30:00Z"
}
```

**可能的错误**：数据库错误。

**示例**：

```js
const d = await Fanke.callHook('store.export')
const blob = new Blob([JSON.stringify(d, null, 2)], { type: 'application/json' })
const a = document.createElement('a')
a.href = URL.createObjectURL(blob)
a.download = `${Fanke.slug}-store.json`
a.click()
```

**注意**：`format` / `plugin` / `exported` 是元信息，`store.import` 只消费 `items`；把整个导出对象传给 `store.import` 不会被识别，需要取 `d.items`。

#### `store.dedup`

**用途**：删除重复 key 的记录（同 key 保留 `id` 最小的一条），并返回删除数与当前总数。属于历史数据/异常兜底。

**参数**：无。

**返回**：

```json
{ "removed": 0, "total": 131 }
```

**可能的错误**：数据库错误。

**示例**：

```js
const r = await Fanke.callHook('store.dedup')
if (r.removed > 0) Fanke.emit('toast', { message: `清理了 ${r.removed} 条重复记录` })
```

**注意**：`k` 本身有 UNIQUE 约束，正常情况下 `removed` 恒为 0；限流 10 次/分钟，不要放进页面加载流程。

#### `store.delete`

**用途**：按 key 列表删除记录。

**参数**：

| 字段 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `keys` | string[] | 是 | — | 非空；一次最多 1000 个 |

**返回**：

```json
{ "deleted": 2 }
```

**可能的错误**：

- `keys required`：payload 解析失败或数组为空。
- `一次最多删除 1000 条`
- 数据库错误。

**示例**：

```js
await Fanke.callHook('store.delete', { keys: ['/uploads/old.jpg'] })
```

**注意**：`deleted` 是实际影响行数，传入不存在的 key 不会报错。删除不可恢复，建议插件先 `store.export` 备份。

#### `store.stats`

**用途**：返回本插件记录总数。

**参数**：无。

**返回**：

```json
{ "total": 131 }
```

**可能的错误**：数据库错误（如表不存在）。

**示例**：

```js
const d = await Fanke.callHook('store.stats')
document.getElementById('total').textContent = d.total
```

**注意**：未建表时 `count(*)` 会报错，初始化流程务必先 `store.ensure_table`。

---

### 4.2 媒体库 `media.list`

**用途**：读取媒体库文件列表，供扫描类插件为图片补配置、清理无效引用等。

**参数**：

| 字段 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `mime_prefix` | string | 否 | `""` | mime 前缀，如 `"image/"`；空表示全部。长度 ≤64，且不能包含 `$`、`;`、`'`、`"`、`\` |

**返回**：

```json
{
  "items": [
    {
      "id": 42,
      "filename": "cat.jpg",
      "storage": "local",
      "key": "2026/11/abcdef1234567890.jpg",
      "url": "/uploads/2026/11/abcdef1234567890.jpg",
      "mime": "image/jpeg",
      "size": 183421,
      "width": 1600,
      "height": 900,
      "sha256": "9f2c…",
      "created_at": "2026-11-01T12:00:00Z"
    }
  ],
  "total": 1
}
```

**可能的错误**：

- `payload 解析失败`：payload 非空但不是合法 JSON。
- `mime_prefix 非法`：超过 64 字符或含危险字符。
- 数据库错误。

**示例**：

```js
const d = await Fanke.callHook('media.list', { mime_prefix: 'image/' })
const urls = d.items.map(m => m.url)
```

**注意**：

- 最多返回 5000 条（`ORDER BY id LIMIT 5000`），`total` 是本次返回条数，不是媒体库总数；没有分页/offset 参数，超大媒体库只能拿到前 5000 条。
- 不区分 `storage`（本地与 S3/远端记录都可能出现）；`width` / `height` 可能为 0。
- 只读，不返回派生图（variants）。

---

### 4.3 网络 `http.check_url`

**用途**：服务端探测一组 URL 的可达性，适合“清理无效外链图”等场景。

**参数**：

| 字段 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `urls` | string[] | 是 | — | 非空；一次最多 100 条 |

**返回**：

```json
{
  "reachable": {
    "/uploads/local.jpg": true,
    "https://example.com/a.jpg": true,
    "https://example.com/dead.jpg": false
  }
}
```

**可能的错误**：

- `urls required`：payload 解析失败或数组为空。
- `一次最多探测 100 条`

**示例**：

```js
const urls = list
const d = await Fanke.callHook('http.check_url', { urls })
const dead = urls.filter(u => d.reachable[u] === false)
```

**注意**：

- 站内相对路径（以 `/` 开头且不是 `//`）**直接判为可达，不发任何请求**。
- 外部地址仅允许 `http://` / `https://` 且 host 非空；其他 scheme、空串、超长（>2048）直接判 `false`。
- 探测流程：先 `HEAD`；返回 405 / 501 时回退 `GET`（带 `Range: bytes=0-0`）；状态码 `< 400` 判可达；单条超时 5 秒（HTTP 客户端 6 秒），整体并发上限 8。
- **严格禁止探测内网**：字面量回环、私网、链路本地（含云元数据 `169.254.169.254`）、CGNAT、组播、保留/文档网段直接判 `false`；域名在**建立连接时**解析并逐个校验真实 IP（防 DNS 重绑定），每次重定向也重新校验。探测客户端不读取环境代理，避免经代理绕过校验。
- 返回是 map：重复 URL 会被覆盖；非法 URL 也以 `false` 出现在结果里，便于前端对齐。
- 插件侧建议与浏览器 `new Image()` 探测做“双端都不可达才判无效”，避免服务端网络策略造成误删。

---

### 4.4 AI `ai.chat`

**用途**：调用后台「AI 连接」中配置的模型。后端只负责挑选连接、转发请求；**提示词与结果解析完全由插件负责**，后端不含任何具体业务提示词。

**参数**：

| 字段 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `messages` | `{role, content}[]` | 是 | — | 1–50 条；`role` 仅限 `system` / `user` / `assistant`；单条 `content` 不能为空；全部内容合计 ≤200000 字符（按 rune 计） |
| `group` | string | 否 | — | 限定在某个 AI 分组内选择 |
| `model` | string | 否 | — | 进一步锁定组内某个模型 |
| `temperature` | number | 否 | 0 | 允许 0–2 |
| `max_tokens` | number | 否 | 0 | 允许 0–32000 |

`group` 与 `model` 都为空时，后端会套用后台 AI 连接页保存的「当前使用」选择；该选择也为空时，在全部启用连接中按权重负载均衡。只要传了其中任意一个，就按请求原样使用，不再套用全局选择。

**返回**：

```json
{
  "content": "模型回复文本",
  "model": "gpt-4o-mini",
  "usage": { "prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0 }
}
```

**可能的错误**：

- `参数格式错误`：payload 不是合法 JSON。
- `messages 不能为空` / `messages 最多 50 条`
- `messages[<i>] role 非法` / `messages 内容不能为空` / `messages 内容过长`
- `temperature 须在 0-2 之间` / `max_tokens 须在 0-32000 之间`
- `AI 服务不可用`：后端未初始化 AI 客户端。
- `没有匹配的 AI 连接,请检查分组/模型与启用状态`：分组/模型无匹配或全部未启用。
- 上游接口错误会原样返回错误文案。

以上错误在 HTTP 层统一是 **400**（不是 409/500）。

**示例**：

```js
const r = await Fanke.callHook('ai.chat', {
  messages: [
    { role: 'system', content: '你是中文技术博客编辑。' },
    { role: 'user', content: '给这篇文章拟 3 个标题。' }
  ],
  group: '主力模型',
  temperature: 0.4,
  max_tokens: 800
})
console.log(r.content, r.usage.total_tokens)
```

**注意**：

- 限流只有 20 次/分钟，重试要退避；不要按输入字符实时调用。
- 建议把 AI 结果做成“用户确认后应用”，避免直接改写文章。
- `usage` 字段是否全为 0 取决于上游是否返回用量；不要用它做计费依据。

---

### 4.5 通知 `notify.send`

**用途**：请求宿主在指定范围显示通知。后端只做参数校验与默认值补全，**实际渲染由宿主前端完成**（插件 iframe 不能直接操作父页面 UI）。

**参数**：

| 字段 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `message` | string | 是 | — | 去除首尾空白后不能为空，≤2000 字（按 rune） |
| `title` | string | 否 | `""` | 去除首尾空白后 ≤120 字 |
| `level` | string | 否 | `info` | 只能是 `info` / `success` / `warning` / `error` |
| `scope` | string | 否 | `global` | `global` 全局通知 / `inline` 插件区域内提示 / `modal` 弹出框 |
| `duration` | number | 否 | `3000` | 仅 `global` / `inline` 有效；小于 1000 按 1000，大于 15000 按 15000；`modal` 强制为 0 |

**返回**：补全默认值与裁剪后的同一份参数：

```json
{
  "message": "保存成功",
  "title": "",
  "level": "success",
  "scope": "global",
  "duration": 3000
}
```

**可能的错误**：

- `参数格式错误`
- `message 不能为空` / `message 不能超过 2000 字`
- `title 不能超过 120 字`
- `level 只能是 info/success/warning/error`
- `scope 只能是 global/inline/modal`

**示例**：

```js
await Fanke.callHook('notify.send', {
  level: 'error',
  title: 'AI 调用失败',
  message: '所有 AI 连接均失败：\n主力/gpt-4o-mini: 401 unauthorized',
  scope: 'modal'
})
```

**注意**：

- 宿主行为：`global` 用 Element Plus 通知（右上角，带 duration）；`inline` 在插件 iframe 上方渲染可关闭的提示条（忽略 duration）；`modal` 用 `ElMessageBox.alert` 弹出，需用户点“知道了”。
- 在插件详情页与文章编辑器面板中都会由宿主渲染；普通前台页面（不在插件宿主内）没有通知桥。
- 简单 toast 仍可用 `Fanke.emit('toast', { message, ok })`；需要标题、级别、弹出确认时用 `notify.send`。

---

## 五、生命周期事件

生命周期事件由后端在真实业务操作中触发，**没有 HTTP 入口，也不能通过 `Fanke.callHook` 主动调用**（`InvokeHook` 会直接拒绝：`生命周期事件不可手动调用: <name>`）。它们只分发给编译进后端的 Go 处理器。

### 5.1 事件列表与触发时机

| 事件 | 触发时机 | 处理器错误的影响 | 主要 payload 字段 |
| --- | --- | --- | --- |
| `beforePostCreate` | 创建文章：Markdown 已渲染为 HTML、写库之前 | **中止保存**，HTTP 422 | `PostID=0`、`Post`、`ContentHTML` |
| `afterPostCreated` | 文章插入成功、返回响应之前 | 仅记日志 | `PostID`（新 id）、`Post`、`ContentHTML`（已消毒） |
| `beforePostUpdate` | 更新文章：写库之前 | **中止保存**，HTTP 422 | `PostID`、`Post`、`OldPost`、`ContentHTML` |
| `afterPostUpdated` | 文章更新成功之后 | 仅记日志 | `PostID`、`Post`、`OldPost`、`ContentHTML` |
| `beforePostDelete` | 删除文章：写库之前 | **中止删除**，HTTP 422 | `PostID`、`OldPost`、`ContentHTML` |
| `afterPostDeleted` | 文章删除成功之后 | 仅记日志 | `PostID`、`OldPost`、`ContentHTML` |
| `beforeMediaUpload` | 媒体上传：去重判断、派生处理、落盘之前 | **中止上传**，HTTP 422 | `Media`（`Filename`/`Size`/`SHA256`/`Storage`） |
| `afterMediaUploaded` | 原图成功且派生处理结束（含复用已有媒体的分支） | 仅记日志 | `Media`、`Variants`、`Reused` |
| `beforeMediaDelete` | 删除媒体及其派生图之前（单条、批量、同步清理） | **中止删除**；批量接口逐条记失败 | `Media`、`Variants` |
| `afterMediaDeleted` | 媒体数据库记录删除之后 | 仅记日志 | `Media`、`Variants` |

补充规则（与代码一致）：

- 文章的保存顺序固定为：`before*` → `SanitizeHTML` → 数据库写入 → 成功后 `after*`。前置处理器**不能绕过消毒**。
- 只有 `beforePostCreate` / `beforePostUpdate` 中处理器对 `ContentHTML` 的修改会被采纳；`Post`、`OldPost`、`Media`、`Variants` 等字段的修改一律丢弃。
- `beforeMediaUpload` 在去重判断之前触发，因此“复用已有媒体”的分支同样会先收到 `beforeMediaUpload`；其后的 `afterMediaUploaded` 会用 `Reused=true` 表示复用。
- 批量删除（`POST /admin/media/batch-delete`）对每个 id 依次走 `beforeMediaDelete` / `afterMediaDeleted`；前置拒绝只让该条失败，不中断其他条目。
- S3 同步清理（`/admin/media/sync/apply`）删除记录时也触发删除事件，但 payload 只有 `Media`，没有 `Variants`。
- S3 同步导入、存储转移、文章快捷发布/公开/置顶等操作**不会**触发这些事件。
- `postCreate` / `postUpdate` 等数据库阶段不是生命周期事件，不可订阅、不可调用。

### 5.2 `LifecycleData` 字段

```go
type LifecycleData struct {
    PostID      int64
    Post        *repo.PostInput
    OldPost     *repo.Post
    ContentHTML string
    Media       *repo.Media
    Variants    []repo.MediaVariant
    Reused      bool
}
```

| 字段 | 类型 | 说明 | 哪些事件有值 |
| --- | --- | --- | --- |
| `PostID` | int64 | 文章 ID；创建前置事件为 0 | 所有文章事件（创建 before 为 0） |
| `Post` | `*repo.PostInput` | 待写入的文章快照 | `beforePostCreate`、`afterPostCreated`、`beforePostUpdate`、`afterPostUpdated` |
| `OldPost` | `*repo.Post` | 更新/删除前的完整文章快照 | `beforePostUpdate`、`afterPostUpdated`、`beforePostDelete`、`afterPostDeleted` |
| `ContentHTML` | string | 待保存（或已保存）的正文 HTML | 所有文章事件 |
| `Media` | `*repo.Media` | 媒体对象；上传前置事件只有 `Filename`、`Size`、`SHA256`、`Storage` 有值 | 所有媒体事件 |
| `Variants` | `[]repo.MediaVariant` | 派生图记录（上传成功后或删除前）；同步清理删除时为空 | `afterMediaUploaded`、`beforeMediaDelete`、`afterMediaDeleted` |
| `Reused` | bool | 上传是否复用了已有媒体 | `afterMediaUploaded` |

没有语义的字段为零值（`nil` / `0` / `""` / `false`）。每个处理器收到的是**独立深拷贝快照**：处理器对 `ContentHTML` 之外的字段（`PostID`、`Post`、`OldPost`、`Media`、`Variants`、`Reused`）的修改不会传播给后续处理器，也不会写回业务对象；唯一例外见 5.4 第 8 条（`beforePostCreate` / `beforePostUpdate` 的 `ContentHTML` 会逐个处理器累积生效）。

### 5.3 处理器注册方式（Go 侧）

```go
package app

import (
    "context"
    "strings"

    "fanke/backend/internal/repo"
)

func init() {
    RegisterLifecycleHandler("my-plugin", BeforePostUpdate,
        func(ctx context.Context, ext repo.Extension, data *LifecycleData) error {
            data.ContentHTML = strings.ReplaceAll(data.ContentHTML, "old", "new")
            return nil
        })
}
```

签名：

```go
type LifecycleHandler func(context.Context, repo.Extension, *LifecycleData) error

func RegisterLifecycleHandler(slug, event string, fn LifecycleHandler)
```

- `slug` 必须与插件 `info.json` 的 slug 一致；`event` 必须是已知生命周期事件；`fn` 不能为空，否则 panic。
- 同一 `slug + event` 重复注册会 panic。
- 处理器随后端编译注册，**不会加载或执行插件包内的 JavaScript**；第三方插件包里的 JS 没有服务端执行能力。

### 5.4 分发规则

`dispatchLifecycle` 的分发条件与顺序：

1. 事件名必须合法，否则返回 `unknown lifecycle event: <event>`。
2. 从数据库读取全部插件，按 slug 升序依次处理。
3. 跳过未启用（`enabled=false`）或未初始化（`initialized=false`）的插件。
4. 跳过没有在 `request` 中声明该事件的插件。
5. 跳过没有注册 Go 处理器的插件（声明 ≠ 处理器）。
6. 每个处理器执行前做一次 `LifecycleData` 与 `repo.Extension` 深拷贝。
7. 处理器返回错误时：
   - 前置事件（`beforePostCreate` / `beforePostUpdate` / `beforePostDelete` / `beforeMediaUpload` / `beforeMediaDelete`）：立即中止后续处理器，错误向上返回，业务操作被拒绝；
   - 后置事件：写日志后继续，**不会反转已经成功的操作**。
8. 只有 `beforePostCreate` / `beforePostUpdate` 处理器返回后，其 `ContentHTML` 会回写到事件数据：后续处理器收到的是前者修改后的 HTML，最终结果被采纳。这一累积仅限这两个事件的 `ContentHTML`。

另外，前置阶段如果连插件列表都读不出来，也会直接中止业务（视为无法完成检查）；后置阶段读取失败只记日志。

### 5.5 为什么不能用 `callHook` 触发

- 生命周期事件与能力 API 是两套注册表：`hookRegistry`（能力）与 `lifecycleRegistry`（处理器）。`RegisterHook` 明确禁止使用生命周期事件名（panic）。
- `InvokeHook` 第一道检查就是 `lifecycleEvents[name]`，命中直接报错，任何通道（管理端/公开）都无法触发。
- 事件必须由真实业务动作（保存文章、上传媒体、删除媒体）产生，携带的是后端内存中的业务快照；客户端伪造的 payload 无法替代。
- 声明即订阅，但订阅不等于实现：`request` 里写 `beforePostCreate` 只表示“我愿意接收”，真正执行取决于后端是否 `RegisterLifecycleHandler` 了该 slug + 事件。当前只有内置 `image-viewer` 注册了 `beforePostCreate` / `beforePostUpdate`（用于规范化 `<fanke-img>` 标签）。

### 5.6 内置处理器示例

后端内置 `image-viewer` 在 `beforePostCreate` 与 `beforePostUpdate` 中调用 `render.NormalizePluginImages(data.ContentHTML)`，补全/规范化插件图片标签。它演示了前置事件的标准用法：只修改 `ContentHTML`，不碰其他字段，也不依赖浏览器环境。

---

## 六、声明式扩展点

声明式扩展点在 `info.json` 的 `request` 中声明，通过 `ValidateHooks` 校验（存在 `declarativeEvents` 中即可），但**不提供任何可调用函数**：它们只影响宿主前端是否渲染入口、是否加载插件脚本。调用 `Fanke.callHook('addEditorPanel')` 会得到 `hook 不存在: addEditorPanel`。

四个扩展点都需要插件满足“已安装 + 已初始化 + 已启用 + 已声明”。

### 6.1 `addSettingsItems`

**作用**：在后台「设置」页挂载插件配置入口。

**宿主行为**：后端 `/api/v1/admin/extensions/contributions` 返回 `settings` 数组，设置页为每项渲染一张卡片（标题 + 说明 + “打开插件配置 →”链接），链接指向 `/admin/plugins/{slug}`，配置 UI 与保存逻辑由插件详情页 iframe 自己实现。

**插件包内需要提供**：`index.html`（详情页配置界面）+ 正常的 store hook 读写配置，不需要额外脚本。

**声明方式**：

```json
{ "request": ["store.ensure_table", "store.get", "store.save", "addSettingsItems"] }
```

**注意**：当前设置项来自后端编译期注册的 `mediaPlugins` 表（`image-processing`、`s3-storage`），只有声明了 `addSettingsItems` 才会保留设置贡献；**普通插件仅声明 `addSettingsItems` 不会自动出现设置卡片**，需要在 Go 侧为该 slug 注册 `SettingContribution`。

### 6.2 `addUploadMediaMenuItem`

**作用**：在媒体库/上传组件的存储选择菜单中挂载“上传至某存储桶”选项。

**宿主行为**：`contributions.upload_menu` 返回条目，前端 `useMediaUpload` 过滤出 `storage === 's3'` 或匹配 `^s3:[^\s:]+$` 的项，按 `is_default` 置顶，与“本地”一起构成下拉选项；选中后上传请求携带 `storage` 表单字段。

**插件包内需要提供**：无需额外文件；上传能力由后端编译期注册的存储 provider 提供。

**声明方式**：

```json
{ "request": ["beforeMediaUpload", "addUploadMediaMenuItem"] }
```

**注意**：

- 条目不是由插件自定义文案/存储名生成的：后端按已配置的 S3 桶逐桶生成，`label` 固定为 `上传至<桶别名>`，`storage` 为 `s3:<桶 id>`（旧版单桶配置为 `s3`），`is_default` 标记默认桶，凭据不全的桶 `enabled=false`。
- 只有编译期注册了 provider 的插件（当前为 `s3-storage`）才会产出条目；普通插件仅声明不会生效。
- 未声明 `beforeMediaUpload` 时，该插件的 processor/provider 会被禁用（`activeMediaPlugins` 中置空），对应条目也不会出现。

### 6.3 `addEditorPanel`

**作用**：在文章编辑器里为插件渲染一个工具按钮，点击后以抽屉内的同源 iframe 打开插件面板。

**宿主行为**：

- 后端 `GET /api/v1/admin/extensions/editor-panels` 返回 `{ panels: [{ plugin_slug, name, description }] }`，只包含满足条件的插件。
- 编辑器渲染按钮（文案为插件 `name`），点击打开右侧抽屉，iframe 地址为 `/api/v1/ext/{slug}/page?panel=1`。
- 打开时及标题/摘要/正文/选中文本变化时，宿主向 iframe 推送：

  ```js
  { type: 'fanke:editor:content', payload: { title, summary, content, selection, post_id } }
  ```

- 插件 → 宿主消息：

  ```js
  // 应用标题/摘要（allow_empty 允许回填空串，用于撤销）
  parent.postMessage({ type: 'fanke:editor:apply', payload: { title, summary, allow_empty: false } }, location.origin)

  // 用纯文本替换当前选中内容
  parent.postMessage({ type: 'fanke:editor:apply', payload: { content_text: '…' } }, location.origin)

  // 撤销编辑器最近一次改动
  parent.postMessage({ type: 'fanke:editor:undo' }, location.origin)
  ```

- 面板内的 `Fanke.callHook` 仍然走父页面代发；`store.ensure_table` / `store.save` / `store.import` / `store.dedup` / `store.delete` 会触发宿主“保存中…”全屏遮罩。
- `notify.send` 的 `inline` 通知会渲染在抽屉内 iframe 上方。

**插件包内需要提供**：`index.html` 能识别 `?panel=1` 并渲染精简面板（不要假设拥有整页 DOM）。

**声明方式**：

```json
{ "request": ["store.ensure_table", "store.get", "store.save", "ai.chat", "notify.send", "addEditorPanel"] }
```

**注意**：`addEditorPanel` 与 `editor.register` 都按数据库中的插件记录通用发现（不限于内置插件）；但面板本身只是 iframe，宿主不会替插件执行任何业务逻辑。

### 6.4 `editor.register`

**作用**：让后台富文本编辑器加载插件的 `editor.js`，插件通过 `window.FankeEditor.registerNode()` 接管某种自定义标签的插入菜单、画布渲染与属性面板。

**宿主行为**：

- 后端 `GET /api/v1/ext` 的 `editors` 数组包含所有“已启用 + 已初始化 + 声明 `editor.register`”的 slug。
- 后端编辑器首次加载时，为每个 slug 注入 `<script src="/api/v1/ext/{slug}/page/editor.js">`；加载失败（404 等）直接跳过，不影响编辑器本体。
- 注入前宿主会安装 `window.FankeEditor = { registerNode }`。

**插件包内需要提供**：`editor.ts`（安装时自动编译为 `editor.js`，运行时也会按需编译只有 `.ts` 的插件），脚本内调用：

```js
window.FankeEditor.registerNode({
  tag: 'fanke-img',                 // 落库标签名
  label: '图片',
  insertLabel: '插件图片（图注 / 位置 / 样式）',
  defaultAttrs: () => ({ des_pos: 'middle bottom' }),
  attrsFromMedia: (media) => ({ src: media.url, alt: media.filename }),
  toEditor: (attrs, caption) => '<figure data-fanke-node="fanke-img">…</figure>',
  fromEditor: (el) => ({ attrs: { src: el.getAttribute('src') || '' }, caption: el.textContent || '' }),
  panel: {
    sections: [
      { title: '图注', fields: [
        { key: 'caption', label: '图注文字', type: 'text' },
        { key: 'color', label: '文字颜色', type: 'color' }
      ] }
    ]
  },
  read: (el) => ({ caption: el.textContent || '', color: '' }),
  apply: (el, values) => { /* 把面板值写回画布节点 */ }
})
```

`registerNode` 的字段：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `tag` | 是 | 落库标签名；缺失则注册被忽略 |
| `label` | 建议 | 面板/菜单显示名 |
| `insertLabel` | 否 | 插入菜单文案，默认同 `label` |
| `defaultAttrs` | 否 | 插入时的默认属性 |
| `attrsFromMedia` | 否 | 从媒体库选中后构造属性，默认 `{ src }` |
| `toEditor` | 是 | 画布 HTML，返回的根元素必须带 `data-fanke-node="<tag>"`；缺失则注册被忽略 |
| `fromEditor` | 是 | 从画布节点读回 `{ attrs, caption }`；缺失则注册被忽略 |
| `panel` | 否 | 属性面板 schema，`fields[].type` 支持 `text` / `number` / `color` / `select` / `checkbox` |
| `read` | 建议 | 从画布节点读取面板值 |
| `apply` | 建议 | 把面板值写回画布节点 |

**声明方式**：

```json
{ "request": ["store.ensure_table", "store.save", "store.get", "editor.register"] }
```

**注意**：

- **必须声明 `editor.register`**，仅提供 `editor.ts` 不会被加载（`editors` 列表由声明决定）。
- 脚本按单文件设计：后端只做类型擦除、不打包，`import` 不会被解析或合并，跨文件依赖请自己保证加载顺序。
- 多个插件可共存，各注册自己的 `tag`；同一 `tag` 重复注册时后注册的覆盖先注册的。
- 核心编辑器不含任何具体插件知识；未安装插件时注册表为空，插入菜单只有普通 `<img>`。

---

## 七、附录

### 7.1 限流表（完整）

来源：`middleware.go` 的 `hookRateLimits`。维度 `(插件 slug, hook)`，桶容量 = 每分钟上限，管理端与公开通道共用。

| hook | 次/分钟 |
| --- | --- |
| `store.ensure_table` | 5 |
| `store.dedup` | 10 |
| `store.import` | 12 |
| `store.export` | 20 |
| `ai.chat` | 20 |
| `http.check_url` | 30 |
| `store.list` | 60 |
| `store.delete` | 60 |
| `media.list` | 60 |
| `notify.send` | 60 |
| `store.save` | 120 |
| `store.get_batch` | 120 |
| `store.stats` | 120 |
| `store.get` | 300 |
| 其它未列出的 hook（含未来新增） | 默认 60 |

- 生命周期事件与声明式扩展点不经过 hook 限流。
- 超出：HTTP 429、`Retry-After: 60`、文案 `hook 调用过于频繁: <hook>(每个插件上限 <N> 次/分钟)`。
- 进程内内存实现；重启清零；多实例各自计数。

### 7.2 错误对照表

**通道 / 校验级错误**（`invokeExtensionHook` 与 `InvokeHook`）：

| HTTP | 错误文案 | 触发条件 |
| --- | --- | --- |
| 404 | `插件不存在` | slug 未安装 |
| 409 | `插件未启用` | 公开通道调用，但插件 `enabled=false` |
| 429 | `hook 调用过于频繁: <hook>(每个插件上限 <N> 次/分钟)` | 超出该 `(slug, hook)` 配额 |
| 400 | `生命周期事件不可手动调用: <name>` | 用 `callHook`/HTTP 调用生命周期事件 |
| 400 | `hook 不存在: <name>` | 名称未注册（拼写错误、或调用声明式扩展点） |
| 400 | `hook 不开放公开调用: <name>` | 公开通道调用非 `Public` hook |
| 400 | `插件未声明该 hook: <name>` | `request` 中未声明 |
| 400 | `<hook 实现返回的错误>` | 参数/业务/数据库错误（所有 hook 执行错误统一 400） |

**安装级校验**（`POST /admin/extensions`）：

| HTTP | 响应 | 触发条件 |
| --- | --- | --- |
| 422 | `{ "error": "插件声明的 hook 在当前后端不可用,已阻止安装", "missing_hooks": [...] }` | `request` 中存在未注册名称 |

**能力 API 参数错误文案**：

| hook | 错误文案 |
| --- | --- |
| `store.ensure_table` | 数据库错误原样返回 |
| `store.save` | `payload 解析失败,期望 {key, value}`；`key 非法(长度须 1-2048)` |
| `store.get` | `key required` |
| `store.get_batch` | `payload 解析失败`；`一次最多 500 条` |
| `store.list` | 数据库错误原样返回 |
| `store.import` | `payload 解析失败,期望 {items:[{key,value}...], overwrite?}`；`一次最多导入 5000 条` |
| `store.export` | 数据库错误原样返回 |
| `store.dedup` | 数据库错误原样返回 |
| `store.delete` | `keys required`；`一次最多删除 1000 条` |
| `store.stats` | 数据库错误原样返回 |
| `media.list` | `payload 解析失败`；`mime_prefix 非法` |
| `http.check_url` | `urls required`；`一次最多探测 100 条` |
| `ai.chat` | `参数格式错误`；`messages 不能为空`；`messages 最多 50 条`；`messages[<i>] role 非法`；`messages 内容不能为空`；`messages 内容过长`；`temperature 须在 0-2 之间`；`max_tokens 须在 0-32000 之间`；`AI 服务不可用`；`没有匹配的 AI 连接,请检查分组/模型与启用状态`；上游错误文案 |
| `notify.send` | `参数格式错误`；`message 不能为空`；`message 不能超过 2000 字`；`title 不能超过 120 字`；`level 只能是 info/success/warning/error`；`scope 只能是 global/inline/modal` |

> 数据库错误（表不存在、连接失败等）通常以底层错误文本透传，属于非稳定文案，插件不应依赖其内容做分支判断。
