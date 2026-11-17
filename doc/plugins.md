# fanke 插件开发文档

本文档面向插件作者，说明插件包格式、运行时桥接 API，以及目前后端已注册的全部 hook 函数（功能、参数、返回值、示例）。

> **权威 hook 参考见 [`doc/hooks.md`](./hooks.md)**：参数表、返回值、错误、限流、生命周期事件与声明式扩展点都以那份文档为准。本文档保留的是包格式、桥接 API 与使用示例。
>
> 维护规则：新增/修改/删除任何 hook，必须同时更新 `hooks.md` 与本文档；插件使用 hook 必须在 `info.json` 的 `request` 中声明；插件需要新能力时，应在做插件的同时为后端实现 hook 并补文档。

> **术语区分**：本文档的「插件」一律指 `extensions/` 下的扩展包（表 `extensions_data`，接口 `/api/v1/ext*` 与 `/api/v1/admin/extensions*`，后台页面 `/admin/plugins`）。
> 仓库里还有一套**历史遗留的内联 CSS/JS 片段**系统：表 `plugins`（`0002_plugins.sql`，字段 name/css/js/enabled/sort），公开读接口 `GET /api/v1/plugins` 由 `useSiteBoot` 注入到文章页，管理接口 `GET/POST/PUT/DELETE /api/v1/admin/plugins` 仍然存在但**后台没有任何页面调用**，只能直接打接口维护。新需求一律走扩展包，不要再往这套片段系统里加东西。

---

## 一、插件包格式

插件是一个 `.zip` 或 `.7z` 压缩包，包内**至少**包含以下三个文件（可以放在任意一层子目录里，安装时会自动拍平）：

```
my-plugin.zip
├── info.json      # 必需,插件元信息
├── init.js        # 必需,首次安装时执行一次(也可为 init.ts,需自行编译为 js)
└── index.html     # 必需,插件详情页中 iframe 展示的内容
```

### info.json

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `name` | ✔ | 插件名称(任意语言) |
| `author` | ✔ | 作者 |
| `version` | ✔ | 版本号,如 `"1.0"` |
| `date` | ✔ | 发布日期,如 `"2026.10.19"` |
| `request` | ✔ | 数组,声明本插件要调用的能力 API 或订阅的后端生命周期事件名称。**声明了不存在的 hook 会被拒绝安装**(422 + `missing_hooks` 列表)。声明生命周期事件不等于注册处理器,见下文 |
| `slug` | 可选 | 插件目录名/URL 标识,须匹配 `^[a-z0-9][a-z0-9-]{0,47}$`。**中文名插件必须显式指定**(无法从中文派生) |
| `description` | 可选 | 描述,展示在详情页 |
| `url` | 可选 | 作者主页(展示在详情页,点击跳转) |
| `update_url` | 可选 | 检查更新地址。推荐指向仓库共享清单 `updates.json`(后端自动取 `plugins.<slug>`);也兼容旧的扁平格式 `{version, download_url?, date?}`。配置后列表页出现「检查更新」按钮 |
| `favico` | 可选 | 包内图标文件路径(如 `favico.svg`),详情页/列表页展示 |
| `fallback` | 可选 | 对象,声明本插件写入正文的每个自定义标签的回落模板(标签名 → HTML),见下节。**只要插件会往正文写自定义标签就应该声明**,否则插件停用后正文里的标签会退化成浏览器不认识的裸标签 |

示例：

```json
{
  "name": "图片焦点选择器",
  "slug": "image-focus",
  "author": "電腦玩家Stan",
  "version": "1.0",
  "date": "2026.10.19",
  "description": "为图片选择展示焦点",
  "favico": "favico.svg",
  "request": ["store.ensure_table", "store.save", "store.get_batch", "media.list"]
}
```

### 标签回落账本（`fallback`）

插件往正文里写的自定义标签(如 image-viewer 的 `<fanke-img>`)是**插件的私有约定**,核心不认识。插件被禁用、卸载或文件损坏后,已发布文章里的这些标签必须由核心兜底渲染,否则正文就是一堆裸标签。

为此核心维护一本**自己的**「标签回落账本」(settings 键 `tag_fallbacks`)：安装与更新插件时,按 `info.json` 的 `fallback` 字段把「标签 → HTML 模板」登记进去。**卸载插件不删除登记**——正文里的标签是历史事实,必须永远能渲染。

```json
{
  "fallback": {
    "fanke-img": "<figure class=\"fanke-fallback\"><img src=\"{{src}}\" alt=\"{{caption}}\" loading=\"lazy\"><figcaption>{{caption}}</figcaption></figure>"
  }
}
```

#### 触发条件

公开接口 `GET /api/v1/ext` 的 `enabled` 口径是「已启用 + 已初始化 + 插件文件完好」：

- 手动禁用 / 卸载 → 不在 `enabled` 里；
- 文件损坏(详情页 `index.html` 丢失,或声明了 `editor.register` 却没有 `editor.js`/`editor.ts`)→ 不在 `enabled` 里,同时列进 `unhealthy`；
- 只要某个已登记标签的插件不在 `enabled` 里,渲染端就自动改用账本模板。

这套判定对**任何插件**都成立,核心里没有针对具体插件的回落分支。

#### 占位符

模板里可写 `{{属性名}}` 与 `{{caption}}`,渲染时代入落库标签上的实际值：

| 占位符 | 取值 |
| --- | --- |
| `{{caption}}` | 标签内文本;为空时依次退回 `des`、`alt` 属性 |
| `{{src}}`、`{{任意属性名}}` | 标签上的同名属性,取不到则为空串 |

属性名不区分大小写(`{{SRC}}` 等同 `{{src}}`)。**代入值一律 HTML 转义**,模板本身也已在安装时被 `render.SanitizeHTML` 消毒(script、事件属性、`javascript:` URL 在安装阶段就被剔掉),所以账本渲染没有任何执行面。模板 ≤2KB。

#### 所有权与生命周期

- 标签名须匹配 `^[a-z][a-z0-9-]{0,47}$`；
- **一个标签只能属于一个插件**。别的插件已登记的标签再声明会被拒绝安装(400),避免两个插件抢同一个标签把正文改坏；
- 更新插件时先清掉该插件的旧登记再写入新声明,所以新版本不再声明的标签会自动从账本移除；
- 登记写库失败会回滚整个安装(删目录 + 删记录),不会出现「插件装上了但没回落」。

#### 两个渲染面

| 场景 | 行为 |
| --- | --- |
| 文章页 | `useTagFallbacks().render(html)` 把不在线插件的标签直接替换成模板输出(纯替换,不可交互) |
| 后台可视化编辑器 | 同样的标签被包成**信封节点**:`<div class="fanke-fallback-box" contenteditable="false" data-fanke-fallback="标签名" data-fanke-orig="原样落库标签">模板视觉</div>`。保存时拆信,`data-fanke-orig` 原样还原,插件停用期间编辑正文不会丢标签 |

信封整体 `contenteditable="false"` 是刻意的：里面的视觉只是替身,改它写不回落库标签,与其悄悄丢掉用户的编辑不如直接锁住(暖色虚线框 + 悬停提示)。插件恢复在线后,标签重新交给插件自己的 `toEditor`,信封自动消失。这段时间要改标签本身,可右键「编辑 HTML」或用「工具 → 源代码」——那两个面始终只展示/接收落库形态。

> 核心集成代码在 setup 期通过 `composables/useCoreTagRenderers.ts` 的 `claim(tag)` **认领**自己会渲染的落库标签(`useImageViewer` 认领 `fanke-img`)：标签被认领且插件**在线**时由核心渲染(要合并 `des_sty` 令牌与插件默认配置),账本模板只在插件不在线、或没人认领该标签时兜底。核心里没有任何针对具体插件的分支——新增插件的核心集成代码自己 `claim` 即可。

### info.json 中的 Go 子进程插件字段

以下字段**只属于内置的两个 Go 子进程插件**（`s3-storage`、`image-processing`）。可承载子进程的插件集合在后端 `pluginrpc.ValidateManifest` 里是硬编码白名单，第三方插件无法通过声明这些字段获得子进程能力。

| 字段 | 说明 |
| --- | --- |
| `protocol_version` | 必须等于后端 `pluginrpc.ProtocolVersion`（当前为 `1`） |
| `trusted_process` | 必须为 `true`。子进程**没有沙箱**，继承 api 进程的系统调用与文件权限，因此只对内置插件开放 |
| `entrypoint` | 必须精确等于 `server/bin/${goos}-${goarch}/<slug>-server`。`${goos}`/`${goarch}` 在运行时替换，Windows 下自动补 `.exe`。必须是插件目录内的真实可执行文件，不允许绝对路径与符号链接 |
| `capabilities` | 必须与该 slug 的期望集合完全一致（顺序无关、不可重复、不可增减）。`s3-storage`：`storage.save/open/delete/list/head/setCacheControl/listBuckets/test`；`image-processing`：`image.process` |
| `dependencies` | 宿主必须预装的外部命令，启用时用 `exec.LookPath` 校验。`image-processing` 必须为 `["cwebp"]`，`s3-storage` 必须为空 |

非子进程插件**必须完全不写这五个字段**（或保持零值），否则安装/启用会因 `unsupported server plugin` 失败。

`entrypoint` 指向的二进制是构建产物、不入库（`extensions/*/server/bin/` 已 gitignore）。干净检出后需要先构建，否则子进程能力不可用、相关测试会跳过：

```bash
cd extensions/s3-storage/server      && go build -o bin/$(go env GOOS)-$(go env GOARCH)/s3-storage-server .
cd extensions/image-processing/server && go build -o bin/$(go env GOOS)-$(go env GOARCH)/image-processing-server .
```

### 安装行为

- 上传入口:后台「插件」页,仅接受 `.zip` / `.7z`(.7z 依赖服务器装有 7z 命令)。
- 防滥用限制:包内 ≤200 文件、单文件 ≤12MB、总解压 ≤60MB、上传包 ≤30MB；路径穿越(zip-slip)会被拒绝。
- 重复安装同 slug 插件返回 409。
- 安装后插件处于「未初始化 + 未启用」状态:进入详情页会带 `?init=1` 加载 iframe，此时会自动执行 `init.js`,`init.js` 完成初始化后调用 `Fanke.initDone()`，之后才能「启用」。

---

## 二、运行时桥接 API(`window.Fanke`)

插件的 `index.html` 由后端注入桥接脚本后，通过 **同源 iframe** 展示在后台详情页。宿主不会把登录凭证注入 iframe，后端调用都经 `postMessage` 由父页面代发。

> 安全前提：iframe 与后台同源，插件脚本属于**受信任代码**（同源页面在技术上可以访问宿主的 DOM 与 `sessionStorage`，并且 `editor.js` 会直接在后台页面执行）。因此只应安装来源可信的插件；`info.json` 声明的 hook 只是能力白名单，不是沙箱。

```js
// 调用 hook。返回 Promise,resolve 为 hook 返回的 JSON,reject 为 Error(错误消息)。
Fanke.callHook(name, payload) -> Promise<any>

// 通知宿主:init.js 初始化完成(仅 ?init=1 时需要)。
Fanke.initDone()

// 让宿主弹一条 toast。ok=false 为错误样式。
Fanke.emit('toast', { message: '保存成功', ok: true })

// 只读属性
Fanke.slug    // 当前插件 slug,如 "image-focus"
Fanke.isInit  // 当前是否处于首次安装初始化流程(?init=1)
```

调用规则：

- 插件只能调用自己在 `info.json` 的 `request` 里**声明过**的 hook，否则报错「插件未声明该 hook」。
- 详情页 iframe 里的调用走**管理端通道**(父页面带管理员凭证)，不看插件启用状态——初始化阶段也能正常调用。
- 标记为 **Public** 的 hook 另有一条免登录公开通道(`POST /api/v1/ext/{slug}/hook/{name}`)，供前台页面使用，但**要求插件处于启用状态**。

---

## 三、hook 设计约定

hook 分为两个方向：**能力 API** 由插件通过 `Fanke.callHook(name, payload)` 主动调用后端；**生命周期事件** 由后端在真实业务操作中触发已注册的 Go 处理器，不能用 `Fanke.callHook` 主动派发生命周期。两者均通过 `request` 声明，但声明的含义分别为调用权限和事件订阅。

### 后端生命周期事件

当前支持以下事件（这是当前能力说明，不代表它们均在历史版本 1.1 的日期发布）：

| 事件 | 触发时机 |
| --- | --- |
| `beforePostCreate` | 文章创建保存前，可检查并修改待保存正文 HTML |
| `afterPostCreated` | 文章创建成功后 |
| `beforePostUpdate` | 文章正文更新保存前，可检查并修改待保存正文 HTML |
| `afterPostUpdated` | 文章正文更新成功后 |
| `beforePostDelete` | 文章删除前 |
| `afterPostDeleted` | 文章删除成功后 |
| `beforeMediaUpload` | 媒体上传前（含去重复用分支的前置检查），可阻止本次上传 |
| `afterMediaUploaded` | 媒体上传成功后，包括复用已有媒体的成功分支 |
| `beforeMediaDelete` | 媒体删除前 |
| `afterMediaDeleted` | 媒体删除成功后 |

- 在 `info.json` 的 `request` 中声明事件即声明订阅，例如 `"request": ["store.get", "beforePostCreate", "beforePostUpdate"]`。后端还须使用 Go 的 `RegisterLifecycleHandler(slug, event, fn)` 注册对应处理器；声明本身不会生成处理器。
- 真实注册接口为：`type LifecycleHandler func(context.Context, repo.Extension, *LifecycleData) error`，即 `RegisterLifecycleHandler(slug string, event string, fn LifecycleHandler)`。`LifecycleData` 字段为：`PostID int64`（文章 ID）、`Post *repo.PostInput`（待写入文章快照）、`OldPost *repo.Post`（更新/删除前的旧文章快照）、`ContentHTML string`（待保存正文 HTML）、`Media *repo.Media`（媒体对象）、`Variants []repo.MediaVariant`（媒体派生版本）、`Reused bool`（上传是否复用已有媒体）。字段按事件提供；没有语义的字段为零值。
- 每个处理器收到独立的 `Extension` 与 `LifecycleData` 快照。只有 `beforePostCreate` / `beforePostUpdate` 处理器返回后对 `ContentHTML` 的修改会被采纳；`PostID`、`Post`、`OldPost`、`Media`、`Variants`、`Reused` 以及其他字段修改都会被丢弃。

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

处理器通常与后端一起编译注册（不是压缩包内的 JavaScript）；`context.Context` 用于本次业务操作的取消与超时，返回非 `nil` 错误会中止 `before*` 操作。
- 只有**已初始化、已启用且声明了对应事件**的插件才会触发处理器；仅有声明、没有已注册的后端处理器，不会自动生成逻辑。此规则独立于详情页能力 API 的管理端调用权限。
- `before*` 同步执行，检查失败或返回错误可阻止本次操作；正文前置事件还可修改待保存的 HTML。创建的保存顺序为：`beforePostCreate` → `sanitize` 消毒 → `postCreate` 数据库保存 → 成功后通知 `afterPostCreated`；更新的保存顺序为：`beforePostUpdate` → `sanitize` 消毒 → `postUpdate` 数据库保存 → 成功后通知 `afterPostUpdated`。`postCreate` / `postUpdate` 是已有数据库持久化阶段，不是可独立订阅或调用的生命周期事件。前置修改不会绕过消毒。
- `after*` 仅在业务操作成功后通知，处理器错误写入日志，**不反转已经成功的操作**。
- 单条及批量删除统一走对应删除生命周期；批量操作也不能绕过前置检查和成功后通知。媒体上传事件覆盖实际上传与已有媒体复用的成功分支，**R2 扫描导入不算上传**。
- 发布、公开、置顶等快捷状态操作不算正文更新，不触发 `beforePostUpdate` / `afterPostUpdated`。
- 生命周期不依赖插件详情页是否打开，不通过 iframe 的 JavaScript 监听或订阅来伪装后端事件。`init.js` 与 `index.html` 仍只在浏览器侧运行；注册 Go 处理器不提供任意 JavaScript 执行能力，也不表示第三方插件包中的 JavaScript 可以在服务端运行。

### 能力 API

能力 API 是**通用平台能力**，面向所有插件开放，不为某个插件量身定制。目前分四组：

| 分组 | hook | 说明 |
| --- | --- | --- |
| 数据存储 | `store.*` | **每插件隔离的 KV 数据表**:表名由 slug 自动派生(`ext_data_<slug>`),`k` 为 TEXT UNIQUE 主键,`v` 为任意 JSON。插件之间互不可见,也碰不到系统表 |
| 媒体库 | `media.list` | 读媒体库文件列表(扫描类功能的基础) |
| 网络 | `http.check_url` | 服务端探测 url 可达性(带 SSRF 白名单) |
| AI | `ai.chat` | 按后台配置的 AI 连接/分组调用对话,提示词与结果解析都在插件里 |
| 通知 | `notify.send` | 让宿主在指定范围显示通知：全局通知、插件区域内提示、弹出式通知 |

声明式扩展点（在 `request` 里声明，由宿主前端渲染入口，逻辑仍在插件包内）：

| 声明 | 作用 |
| --- | --- |
| `addSettingsItems` | 在后台设置页挂载插件设置入口 |
| `addUploadMediaMenuItem` | 在媒体上传菜单挂载存储选项 |
| `addEditorPanel` | 在文章编辑器挂载插件面板,宿主以 iframe 打开 `index.html?panel=1` |

安全边界：

- 插件拿不到任意 SQL 能力，只能读写**自己 slug 派生的那张表**。
- `http.check_url` 仅允许 `http(s)://` 和站内相对路径，防内网探测。
- 各类批量接口都有数量上限（见下文）。

> ⚠️ `store.get_batch` 是 Public hook:**插件一旦启用，其 store 内容对访客可读**。不要把敏感数据放进 store。

---

## 四、hook 详细说明

### store 数据存储（10 个）

#### `store.ensure_table`

创建本插件的数据表（`ext_data_<slug>`，字段:`id BIGSERIAL / k TEXT UNIQUE / v JSONB / created_at / updated_at`）及 k 索引。幂等。**应在 init.js 里调用。**

- 参数：无
- 返回:`{ ok: true, table: "ext_data_<slug>" }`

```js
// init.js
Fanke.callHook('store.ensure_table').then(function () {
  Fanke.initDone()
})
```

#### `store.save`

保存一条记录（不存在则插入，存在则覆盖）。

- 参数:`{ key: string, value: any }`(key 长度 1–2048;value 任意 JSON)
- 返回:`{ ok: true }`

```js
Fanke.callHook('store.save', {
  key: '/uploads/2026/cat.jpg',
  value: { cx: 0.42, cy: 0.3, note: '焦点在猫脸上' }
})
```

#### `store.get`

按 key 读取单条记录。

- 参数:`{ key: string }`
- 返回：未找到 `{ found: false }`；找到 `{ found: true, key, value, updated_at }`

```js
var d = await Fanke.callHook('store.get', { key: '/uploads/2026/cat.jpg' })
if (d.found) console.log(d.value)
```

#### `store.get_batch`（公开）

批量读取记录。前台页面走公开通道免登录（插件须已启用）。

- 参数:`{ keys: string[] }`，**一次最多 500 条**
- 返回:`{ items: { [key]: value } }`，未找到的 key 不出现在 items 里

```js
var d = await Fanke.callHook('store.get_batch', { keys: ['/uploads/a.jpg', '/uploads/b.jpg'] })
```

前台实际请求（公开通道，供参考）:

```
POST /api/v1/ext/{slug}/hook/store.get_batch
Content-Type: application/json

{"keys":["/uploads/a.jpg"]}
```

#### `store.list`

列出本插件全部记录（按 id 升序）。

- 参数：无
- 返回:`{ items: [{ key, value, updated_at }...], total: number }`

#### `store.import`

批量导入记录。

- 参数:`{ items: [{ key, value }...], overwrite?: boolean }`，**一次最多 5000 条**
  - `overwrite` 默认 `true`：已有 key 被覆盖；传 `false` 则已有 key 不动（扫描初始化场景用这个）
- 返回:`{ imported: number, skipped: number }`(key 非法、写入失败、或 overwrite=false 时已存在的，计入 skipped)

```js
// 扫描媒体库后为所有图片补默认数据(已有记录不动)
var d = await Fanke.callHook('store.import', { items: items, overwrite: false })
```

#### `store.export`

导出本插件全部记录。

- 参数：无
- 返回:

```json
{
  "format": "fanke-store/export@1",
  "plugin": "image-focus",
  "count": 131,
  "exported": "2026-10-19T02:00:00Z",
  "items": [{ "key": "...", "value": {...}, "updated_at": "..." }]
}
```

```js
var d = await Fanke.callHook('store.export')
var blob = new Blob([JSON.stringify(d, null, 2)], { type: 'application/json' })
// 创建 <a download> 触发浏览器下载;取 d.items 即可直接喂给 store.import
```

#### `store.dedup`

去除重复 key 记录（同 key 保留 id 最小的一条）。k 列本来有 UNIQUE 约束，此 hook 是历史数据/异常兜底。

- 参数：无
- 返回:`{ removed: number, total: number }`（`total` 为去重后剩余记录数）

#### `store.delete`

按 key 列表删除记录。

- 参数:`{ keys: string[] }`，**一次最多 1000 条**
- 返回:`{ deleted: number }`

#### `store.stats`

返回本插件记录总数。

- 参数：无
- 返回:`{ total: number }`

---

### 媒体库（1 个）

#### `media.list`

列出媒体库文件，可按 mime 前缀过滤。最多返回 5000 条。

- 参数:`{ mime_prefix?: string }`，如 `"image/"`；不传返回全部类型
- 返回:`{ items: [Media...], total: number }`,Media 字段:`id / filename / storage / key / url / mime / size / width / height / sha256 / created_at`

```js
var d = await Fanke.callHook('media.list', { mime_prefix: 'image/' })
var urls = d.items.map(function (m) { return m.url })
```

---

### 网络（1 个）

#### `http.check_url`

服务端探测一组 url 的可达性。

- 站内路径(`/...`）直接视为可达，不发请求。
- 外部 url:HEAD 探测，返回 405/501 时回退 GET（带 `Range: bytes=0-0`)；状态码 <400 判可达。
- 仅允许 `http(s)://` 与站内相对路径，其余直接判不可达（防 SSRF）。内网/回环/链路本地/CGNAT/保留地址一律不可达：字面量 IP 直接拒绝，域名在连接时与每次重定向重新解析校验真实 IP。
- 单条 5s 超时，整体 8 并发。

参数:`{ urls: string[] }`，**一次最多 100 条**

返回:`{ reachable: { [url]: true|false } }`

```js
var d = await Fanke.callHook('http.check_url', { urls: list })
var dead = list.filter(u => !d.reachable[u])
```

> 清理无效链接的推荐做法：浏览器侧用 `new Image()` 探测一遍，服务端 `http.check_url` 再探测一遍，**两侧都不可达**才认定无效（或运算保真，避免误删）。

---

### AI（1 个）

#### `ai.chat`

调用后台「AI 连接」中配置的模型。后端只负责挑选连接、转发请求，**提示词与结果解析由插件自己负责**（后端不含任何具体业务提示词）。

参数：

```json
{
  "messages": [{ "role": "user", "content": "…" }],
  "group": "主力模型",
  "model": "gpt-4o-mini",
  "temperature": 0.4,
  "max_tokens": 1600
}
```

- `messages`：1–50 条，role 仅限 `system` / `user` / `assistant`，总长度 ≤ 20 万字符。
- `group`：可选，限定在某个分组内选择。
- `model`：可选，进一步锁定组内某个模型。
- 两者都不传时，套用后台 AI 连接页保存的「当前使用」；该选择也为空时，所有启用连接按权重负载均衡。
- 组内多连接按 `weight` 加权随机，权重越大被选中概率越高。

返回：

```json
{ "content": "模型回复文本", "model": "gpt-4o-mini", "usage": { "prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0 } }
```

```js
var r = await Fanke.callHook('ai.chat', {
  messages: [{ role: 'user', content: '给这篇文章拟 3 个标题' }],
  group: '主力模型'
})
console.log(r.content)
```

> hook 执行错误统一返回 HTTP 400：无匹配连接时文案为「没有匹配的 AI 连接,请检查分组/模型与启用状态」，上游接口错误则原样透传上游错误文案。
>
> 分组的增删在后台「AI 连接器」页完成：可新建空分组、重命名分组、删除分组。删除分组时二选一——连同组内模型一起删除，或把模型迁移到另一个分组（模型、密钥、权重保留）。删除或重命名后，后台保存的「当前使用」会自动改指剩余 / 目标分组，不会留下悬空指向。

---

### 通知（1 个）

#### `notify.send`

请求宿主显示一条通知。后端只校验参数并原样返回，**实际渲染由宿主前端完成**（插件 iframe 里不能直接操作父页面 UI）。

参数：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `message` | ✔ | 通知正文，≤2000 字，支持换行 |
| `title` | | 标题，≤120 字 |
| `level` | | `info` / `success` / `warning` / `error`，默认 `info` |
| `scope` | | `global` 右上角通知（默认）/ `inline` 插件区域内提示 / `modal` 弹出框需确认 |
| `duration` | | `global`/`inline` 的显示毫秒，1000–15000，默认 3000；`modal` 忽略 |

返回：补全默认值后的同一份参数 `{ message, title, level, scope, duration }`。

```js
await Fanke.callHook('notify.send', {
  level: 'error',
  title: 'AI 调用失败',
  message: '所有 AI 连接均失败：\n默认/gpt-4o-mini: 401 unauthorized\n备用/claude-x: 429 rate limited',
  scope: 'modal'
})
```

> `notify.send` 同样受「每插件、每 hook、每分钟」的调用次数限制（默认 60 次/分钟）。

---

### 调用频率限制

所有 hook 都按「插件 + hook」维度限流，超出返回 `429`，错误文案会写明该 hook 每分钟上限，并带 `Retry-After: 60`。

| hook | 次/分钟 |
| --- | --- |
| `store.ensure_table` | 5 |
| `store.dedup` | 10 |
| `store.import` | 12 |
| `store.export` | 20 |
| `http.check_url` | 30 |
| `store.list` / `store.delete` / `media.list` / `notify.send` | 60 |
| `store.save` / `store.get_batch` / `store.stats` | 120 |
| `store.get` | 300 |
| `ai.chat` | 20 |
| 其它未列出的 hook | 60 |

- 桶按插件隔离：同为 `ai.chat`，A 插件刷爆不影响 B 插件。
- 管理端调用与公开通道共用同一个桶，不能用公开接口绕过限制。
- 限流为进程内内存实现，按 slug 计数。

---

### 编辑器面板（声明式）

插件在 `request` 中声明 `addEditorPanel` 后，宿主会在文章编辑器里为该插件渲染一个入口按钮；点击后宿主以抽屉内的同源 iframe 打开 `index.html?panel=1`。面板 UI 与全部业务逻辑留在插件包里，宿主只做消息中转。

宿主 → 插件（iframe 收到，`ev.origin` 为站点 origin）：

```js
{ type: 'fanke:editor:content', payload: { title, summary, content, selection, post_id } }
```

- 打开面板时以及编辑器标题/摘要/正文/选中文本变化时都会推送一次；`selection` 是编辑器当前选中的纯文本（未选中为空串），适合做「选中段落润色」。

插件 → 宿主：

```js
// 请求把建议写回编辑器标题/摘要（用户在面板里点「应用」时发送）
window.parent.postMessage({ type: 'fanke:editor:apply', payload: { title: '…', summary: '…' } }, location.origin)

// 用纯文本替换编辑器当前选中内容（如润色结果）
window.parent.postMessage({ type: 'fanke:editor:apply', payload: { content_text: '…' } }, location.origin)

// 撤销编辑器最近一次改动（润色的「撤销」，走编辑器自带历史栈）
window.parent.postMessage({ type: 'fanke:editor:undo' }, location.origin)

// hook 调用/弹 toast 仍用 Fanke.callHook / Fanke.emit，宿主会自动中转
```

约定：

- 面板内不要假设自己拥有全页 DOM，只渲染面板内容。
- `store.ensure_table`、`store.save`、`store.import`、`store.dedup`、`store.delete` 在面板中调用时，宿主会显示「保存中…」忙碌遮罩。
- 建议把「AI 建议」做成需用户点击确认才回填，不要在推送内容时自动改写标题/摘要。
- `fanke:editor:apply` 的 payload 支持 `allow_empty: true`：允许回填空串，用于插件侧「撤销」把标题/摘要恢复成应用前的值；不带该标记时宿主只接受非空值。
- 宿主收到应用后会立刻把最新标题/摘要回推给面板（`fanke:editor:content`）。插件应保留自己的分析结果与应用状态，不要因为内容变化就清空面板，否则用户无法连续应用多条建议或撤销。

---

## 五、完整示例（插件 index.html 片段)

```html
<script>
// 页面加载后拉统计
Fanke.callHook('store.stats').then(function (d) {
  document.getElementById('total').textContent = d.total
})

// 一键扫描:媒体库所有图片补默认数据
document.getElementById('scanBtn').addEventListener('click', function () {
  Fanke.callHook('media.list', { mime_prefix: 'image/' }).then(function (d) {
    var items = d.items.map(function (m) { return { key: m.url, value: { inited: true } } })
    return Fanke.callHook('store.import', { items: items, overwrite: false })
  }).then(function (r) {
    Fanke.emit('toast', { message: '新初始化 ' + r.imported + ' 条' })
  }).catch(function (e) {
    Fanke.emit('toast', { message: '扫描失败:' + e.message, ok: false })
  })
})
</script>
```

错误处理约定：任何 hook 调用失败都会 reject,`Error.message` 为后端返回的错误文案（参数非法、数量超限、hook 未声明、插件未启用等），直接展示给用户即可。

---

## 六、内置插件实例：图片插入/展示组件（image-viewer)

本插件结合前端展示与后端正文保存前处理：除通用能力 API 外，它依赖**正文渲染管线**、**编辑器预览**与**后端生命周期 Go 处理器**等核心侧集成点。文档同时作为该插件的使用说明。

### 1. 正文语法

在正文 HTML 中书写（替代默认 `<img>` + 说明的写法）:

```html
<fanke-img src="https://…/a.jpg" alt="A" des_pos="middle bottom" des_sty="fs=13,c=#666">图片描述</fanke-img>
```

- 手写时必须**顶格书写**并显式书写闭合 `</fanke-img>`;若内容还要经过 Markdown 渲染(导入的历史正文),行首缩进会被当作代码块原样显示。
- 在可视化编辑器里不必手写:插入后画布显示为 `<figure>` + `<figcaption>`,**图注以画布上 `figcaption` 的实时文字为准**保存回 `<fanke-img>` 标签内文;`des` 属性只作为历史内容的回落来源。
- 插件标签固定为 `<fanke-img>`,与原生 `<img>` 完全分开;正文里的普通 `<img>` 不会因为带 `des` 属性而被误判为插件图片。
- 说明文字写在标签对内;也可用 `des="…"` 属性指定(内文为空时回落到 `des`,再回落到 `alt`)。
- 插件详情页的「替换正文中的普通 &lt;img&gt; 标签」开关开启后,原生 `<img>` 也会套用默认位置与样式;关闭时只处理 `<fanke-img>`。外链(`<a>` 内)的原生图片不会被替换。
- 标签上省略的 `des_pos` / `des_sty` 使用插件详情页配置的默认值;没有配置位置时回落到 `middle bottom`。
- 标签写了 `des_pos` 时整段覆盖默认位置;`des_sty` 则按 **token 合并**:标签写了哪些 token 就覆盖哪些,其余 token 仍取默认配置(例如默认 `b,fs=16`,标签只写 `c=#666` 时仍然加粗且字号 16px)。

### 2. des_pos 词表

空格分隔、任意顺序;默认保存序列为 `middle bottom`（水平在前、垂直在后）：

| token | 含义 |
| --- | --- |
| `top` / `bottom` | 说明在图上 / 图下（默认 bottom) |
| `left` / `middle` / `right` | 说明居左 / 居中 / 居右（默认 middle,兼容旧 token `mid`) |

### 3. des_sty 词表

逗号/空格/分号分隔，key=value 形式（颜色值不要带空格）:

| token | 含义 | 示例 |
| --- | --- | --- |
| `hidden` | 隐藏说明文字 | `des_sty="hidden"` |
| `fs=<px>` | 说明字号 | `fs=13` |
| `c=<color>` | 说明文字颜色 | `c=#666` |
| `bg=<color>` | 说明背景色 | `bg=#f5f5f5` |
| `bd=<color>` | 说明描边（1px solid) | `bd=#ddd` |
| `b` / `i` | 加粗 / 斜体 | `b,i` |
| `w=<%>` | 图片宽度（相对正文栏） | `w=60` |
| `r=<px>` | 图片圆角 | `r=12` |

### 4. 配置存储约定

- 表：`ext_data_image_viewer`（由 `store.ensure_table` 创建）。
- 默认配置存于 key = `config`,初始化值为 `{ "des_pos": "middle bottom", "des_sty": "", "replace_img": false }`。
- 插件详情页（index.html）通过 `store.get` / `store.save` 读写；前台页面通过**公开通道** `store.get_batch` 读取（要求插件已启用）。
- 旧配置 `mid` 与 `middle` 等效;加载配置不会覆盖用户已保存的自定义位置,保存时采用水平在前、垂直在后的序列。

### 5. 后端配套（已完成）

- `image-viewer` 在 `request` 中订阅 `beforePostCreate` / `beforePostUpdate`，由后端注册的 Go 处理器在文章创建、正文更新前规范化插件图片标签，不在 iframe 中注册保存监听。
- **默认位置与样式不在保存时写死**：后端只保证标签闭合与消毒;标签没写 `des_pos` / `des_sty` 时，文章页与编辑器预览都会套用插件详情页当前保存的默认配置。因此修改默认值后，**已发布文章无需重新保存即可生效**。
- 消毒策略允许 `fanke-img` 元素及其 `src` / `alt` / `srcset` / `sizes` / `width` / `height` / `loading` / `decoding` / `data-orig` / `des` / `des_pos` / `des_sty` 属性;图片缩略图改写与 src 收集管线同时覆盖 `fanke-img` 和原生 `img`。
- 规范化结果继续经过 `sanitize`，再由 `postUpdate` 保存到数据库，成功后才发送对应 `after*` 通知。
- goldmark 保持 `html.WithUnsafe()`（原生 HTML 直出）+ bluemonday 消毒的组合不变。

### 5.0 1.6 更新记录（2026.11.17）

- **修复在可视化编辑器里直接改图注不入库**：`editor.ts` 的 `fromEditor` / `read` 此前把画布 `figure` 上的 `des` **属性**当成图注权威来源,而用户在画布里改的是 `figcaption` 的**文字**,于是保存时新文字被旧 `des` 覆盖,写回数据库的仍是旧图注。现在统一由 `captionOf()` 取值:画布 `figcaption` 的实时文字优先,`des` 只作为历史内容的回落,与文章页 `useImageViewer.ts` 的取值顺序一致。
- `render()` 改为**始终**输出 `figcaption`(`des_sty` 含 `hidden` 时用 `display:none`),并且不再往画布 `figure` 上写 `des` 属性。因此清空图注会真的存成空、隐藏图注不再丢失、画布里也能直接点进空图注输入。
- 修复历史内容丢图注：旧版 `render()` 只认标签内文,把图注写在 `des` 属性里的老文章一进编辑器就被清空并保存回库;现在会读出 `des`(再回落 `alt`)显示为 `figcaption`,保存时回写成标签内文。

### 5.1 1.5 更新记录（2026.11.02）

- 插件标签改为独立的 `<fanke-img>说明</fanke-img>`,不再占用原生 `<img>`,避免与普通图片互相干扰。
- 编辑器插图默认写入 `<fanke-img>`;新增「替换正文中的普通 `<img>` 标签」开关,开启后普通图片也按插件样式渲染。
- 存量文章一次性由 `<img>说明</img>` 迁移到 `<fanke-img>说明</fanke-img>`（备份表 `posts_html_bak_fanke_20261102`)。

### 5.2 1.4 更新记录（2026.10.31）

- 修复文章页默认配置读取：公开 hook `store.get_batch` 返回 `{"ok":true,"data":{"items":{…}}}`，前端此前按 `items` 平铺解析，导致默认 `des_sty` 一直为空、未自定义的图片没有任何默认样式。现已兼容 `data.items` 结构。
- `des_sty` 改为默认配置与标签 token 合并：标签只覆盖自己写出的 token，未写的（如 `fs`、`b`）继续沿用插件默认值。

### 5.3 1.3 更新记录（2026.10.31）

- 默认说明位置/样式改为**渲染时套用插件配置**：在插件详情页改了默认值后，已发布文章里没写 `des_pos` / `des_sty` 的图片立即跟着变化，不用逐篇重新保存。
- 后端保存时不再把 `des_pos="middle bottom"` 写死进正文，只补 `</img>` 闭合；标签显式写的属性仍然优先。
- 历史文章里由旧版本自动写入的默认 `des_pos="middle bottom"` 已一次性清理（备份表 `posts_bak_20261031`）。

### 6. 前端集成点（核心代码，非插件包内）

- `composables/useImageViewer.ts`：插件默认配置拉取 + **文章页** HTML 转换（`transform`)。文章页 `pages/posts/[slug].vue` 对 `content_html` 应用 `iv.transform(fb.render(html))`；插件页勾选「替换默认 img 标签」(`replace_img`) 后普通 `<img>` 也按插件样式渲染（无 src、位于 `<a>` 内的除外），未勾选时只有 `<fanke-img>` 走插件。
- `utils/tagFallback.ts` + `composables/useTagFallbacks.ts`：通用回落渲染端，按后端账本处理**所有**插件的自定义标签（不认识具体插件）。`useImageViewer` 通过 `composables/useCoreTagRenderers.ts` 认领了 `fanke-img`，所以它在线时仍走上面的 `transform`，只有不在线时才用账本模板。`enabled` / `editors` / `fallbacks` 三者由 `composables/useExtensions.ts` 一次 `/ext` 拉齐并共享，不再各自请求。
- 编辑器不再自己预览 `<fanke-img>`：`components/admin/RichTextEditor.vue`（TinyMCE 宿主）只负责**落库形态 ⇄ 画布形态**互转与选区/面板事件，具体怎么渲染、怎么读写属性全部由插件 `editor.ts` 注册的 `toEditor` / `fromEditor` / `read` / `apply` 决定（见下文「11. 编辑器能力注册」）。落库形态绝不回灌编辑器,否则会被解析成裸标签并丢属性。
- **所有 HTML 文本界面只露落库形态的标签**：右键「编辑 HTML」的代码块与「工具 → 源代码」对话框展示/接收的都是 `<img …>` / `<fanke-img …>说明</fanke-img>`，画布的 `<figure data-fanke-node>` 包装是内部表示，不外露给用户写；应用时再转回画布形态。因此未勾选替换时手打 `<img>` 保持原生、手打 `<fanke-img>` 才走插件，勾选后两者在文章页都按插件渲染。
- `components/admin/PostEditor.vue`：全页共用一个媒体选择对话框,插入插件节点时由 `insert-node` 事件打开,选中后调用 `insertNode(tag, media)` 写入画布;保存走 `getContent()`(内部即 `fromEditorHtml`)。
- 文章页样式类名前缀 `iv-`，定义在 `assets/css/global.css`；回落态用中性的 `.fanke-fallback`（同样在 `global.css`，只给排版，不猜插件外观）；编辑器画布内的样式由 `editor.ts` 内联生成，回落信封的样式在 `RichTextEditor.vue` 的 `content_style` 里，两边互不共用。

### 7. 1.2 更新记录（2026.10.27）

- 统一默认 `des_pos` 为 `middle bottom`（水平居中、垂直图下）,兼容旧配置 `mid`。
- 显式闭合的 `<img>说明</img>` 可省略 `des_pos` 和 `des_sty`,仍应用插件默认效果;普通未闭合且无 des 系属性的图片保持原样。
- 优先级保持为标签显式属性 > 用户保存的全局默认配置 > 内置默认值;不覆盖用户自定义位置,插件未启用时文章页不转换。
- 插件详情页同步展示 1.2 更新记录,并更正文章页仅靠 des 系属性识别及服务端丢弃闭合标签的过时说明。

### 8. 1.1 更新记录（补录，2026.10.23）

- 增加更多 Hook 扩展点，支持保存前检查与操作成功后的扩展。
- 本条为历史日志补录；上文列出的完整生命周期与 image-viewer 保存前规范化描述的是当前实现，不表示所有当前事件均在 2026.10.23 发布。
- 当前版本仍为 **1.2**，保留 2026.10.27 的 1.2 更新记录。

### 9. 安全边界（2026.11.14 补充）

- **插件服务端程序没有沙箱**：pluginrpc 直接以 anke 用户身份 exec 插件自带的二进制，拥有与该服务相同的文件读写权限（媒体卷、主题目录、扩展目录，以及通过 API 能触达的数据库）。
- 因此**只应安装来源可信的插件包**。上传扩展需要管理员权限，但管理员在安装前仍应确认包内 server/ 目录的可执行文件来源。
- 系统级缓解目前在 anke-api.service 中完成：ProtectSystem=strict、ProtectHome=yes、PrivateDevices=yes、RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX、MemoryMax=512M。这些约束对子进程同样生效。
- 公开 hook 仅允许 Public=true 的 hook 被匿名调用，并按「插件 + hook」限流；管理端 hook 必须带 Bearer。

### 10. 脚本语言：TypeScript（2026.11.14 起）

- **新插件一律用 TypeScript 编写**，init.ts / pp.ts 等源文件直接放进插件包，安装时由后端用 esbuild 自动编译成同名 .js；.ts 源码会一并保留，方便二次修改。
- 上传包内允许混合 .js 与 .ts；同名时以 .ts 编译产物为准（覆盖同名 .js）。
- index.html 里 <script src="./app.ts"> / <link href="./x.ts"> 会在安装时自动改写为 .js 引用，写 .ts 即可。
- 已安装但只有 .ts 的插件，请求 .js 时后端会按需现场编译并落盘缓存，无需重装。
- 约定：插件脚本保持**单文件、无 import**（esbuild 只做类型擦除，不做打包），要复用逻辑就把函数写在同一个文件里。
- 编译失败会在上传接口直接返回 422，并带上 文件:行:列 与错误原因，安装不会半途落盘。
- 内置插件的 TS 源码位于仓库 extensions/<slug>/，服务器上同名目录只保留 index.html、*.ts、info.json、avicon 与服务端 server/，.js 由后端按需生成。

### 11. 编辑器能力注册（2026.11.14 起）

插件包可选提供 editor.ts，声明"这个插件要接管哪种标签、在编辑器里怎么显示、属性面板有哪些字段"：

```js
window.FankeEditor.registerNode({
  tag: 'fanke-img',            // 落库标签名
  label: '图片',
  insertLabel: '插件图片（图注 / 位置 / 样式）',
  defaultAttrs: () => ({ des_pos: 'middle bottom' }),
  toEditor: (attrs, caption) => '<figure data-fanke-node="fanke-img">…</figure>',
  fromEditor: (el) => ({ attrs: { src: '…' }, caption: '…' }),
  panel: { sections: [ { title: '图注', fields: [
    { key: 'caption', label: '图注文字', type: 'text' },
    { key: 'color', label: '文字颜色', type: 'color' },
  ] } ] },
  read: (el) => ({ … }),
  apply: (el, values) => { … },
})
```

规则：
- 核心编辑器不包含任何具体插件知识；未安装插件时注册表为空，插入菜单只有「普通 <img>」。
- 多个插件可共存，各注册自己的 `tag`；同一 `tag` 重复注册时后者覆盖。
- `toEditor` 返回的根元素必须带 `data-fanke-node="<tag>"`，核心据此在保存时回写给 `fromEditor`。
- 面板字段类型支持 `text` / `number` / `color` / `select` / `checkbox`；`color` 自动渲染取色器。
- 插件是否被加载由后端 /api/v1/ext 的 editors 列表决定：**只有 `info.json` 的 `request` 里声明了 `editor.register` 的启用插件才会注入**；只放 `editor.ts` 而不声明不会生效。

### 12. 标签回落（1.7 起）

机制见「一、插件包格式 → 标签回落账本（`fallback`）」。image-viewer 登记的模板：

```json
"fallback": {
  "fanke-img": "<figure class=\"fanke-fallback\"><img src=\"{{src}}\" alt=\"{{caption}}\" loading=\"lazy\"><figcaption>{{caption}}</figcaption></figure>"
}
```

- 插件在线时**不走**这条模板：`fanke-img` 由核心集成代码渲染（`useImageViewer.ts` 要合并 `des_sty` 令牌与插件默认配置，模板做不到），它由 `useCoreTagRenderers().claim('fanke-img')` 认领，模板只作兜底。
- 插件不在线时文章页按模板出「图 + 图注」，编辑器按信封锁定该块（保存原样还原落库标签）。
- 回落态只用标签上的 `src` 与图注，`des_pos` / `des_sty` / 宽度圆角等定制**不生效**——那些语义属于插件本身，核心不猜。
