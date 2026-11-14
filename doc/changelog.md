# fanke 更新记录

本文件记录系统本身的变更（后端 / 前端 / 文档）。插件各自的更新记录写在插件详情页，见文末索引。

---

## 2026.11.14

### 插件

- **编辑器能力注册机制**
  - 新增 `window.FankeEditor.registerNode()`：插件通过 `editor.ts` 注册标签、画布渲染、属性面板 schema、读写逻辑。
  - 核心编辑器不再硬编码 `fanke-img` / `des_pos` / `des_sty`；未装插件时插入菜单只有「普通 <img>」，多插件各注册各的 tag。
  - `/api/v1/ext` 增加 `editors` 列表（存在 `editor.ts`/`editor.js` 的启用插件），编辑器启动时按需注入 `editor.js`。
  - 属性面板改为按 schema 通用渲染，`color` 字段自动使用取色器。
  - image-viewer 的渲染与面板逻辑移入 `extensions/image-viewer/editor.ts`。
- **插件脚本 TypeScript 化**
  - 5 个内置插件（image-focus / image-processing / image-viewer / s3-storage / seo-optimizer）的内联脚本抽成 `app.ts`，`init.js` 改名为 `init.ts`；`index.html` 改为引用 `./app.js`。
  - 后端新增 esbuild 转译：上传包里的 `.ts` 在安装时编译为同名 `.js`（`.ts` 源码保留），`index.html` 中的 `.ts` 引用自动改写为 `.js`；编译失败返回 422 并带 `文件:行:列`。
  - 运行时兜底：只有 `.ts` 没有 `.js` 时，请求 `.js` 现场编译并落盘，老插件无需重装。
  - 约定：插件脚本保持单文件、无 `import`（只做类型擦除，不做打包）。

### 后端

- **鉴权与越权**
  - 未公开文章（草稿）不再凭 `X-Fanke-Internal` 自定义头放行：该头任何客户端都能伪造。现在要求请求携带有效 Bearer（公开读接口加 `optionalBearer`），且草稿预览响应改为 `no-store`，不进入浏览器/CDN 公共缓存。
  - `/admin/settings` 读写移到管理员组：设置项含可执行页脚 HTML、导航与主题，作者角色不得修改，避免越权注入脚本。
  - `JWT_SECRET` 缺失时不再导致登录/鉴权接口 nil panic：启动时生成临时密钥并告警。
- **SSRF**
  - `http.check_url` 拒绝回环/私网/链路本地/CGNAT/组播/保留网段，域名在建立连接时解析校验真实 IP（防 DNS 重绑定），重定向逐跳校验，并不走环境代理。
  - 插件「检查更新」的 `update_url` 改用同一探测客户端，防止把插件元数据指向内网。
- **上传与文件**
  - `.7z` 解包补上与 zip 一致的限制（≤200 文件、单文件 ≤2MB、总量 ≤20MB），并拒绝符号链接/非普通文件，避免软链目标被复制进公开的扩展目录。
  - 媒体 MIME 由扩展名决定，客户端声明的 `Content-Type` 仅在同类时采纳，防止 `.txt` 以 `text/html` 同源返回。
  - 插件 page/asset 路由校验 slug 格式；批量媒体操作与同步修复增加单次数量上限；批量打包下载放宽写超时。
- **敏感字段落库加密**
  - 新增 `internal/secret`：AES-256-GCM，密钥由 `RECOVERY_KEY` 经 SHA-256 派生；密文前缀 `enc:v1:`。
  - `ai_connections.api_key`、`settings.s3_buckets`（access/secret/session token）写入时自动加密、读取时自动解密；启动时把历史明文就地加密（幂等）。
  - **注意**：`RECOVERY_KEY` 一旦变更，历史密文将无法解密（读取会退回原值）。换密钥前需先导出再重录。
- **`/img` 缩图宽度限定档位**
  - 请求宽度向上吸附到固定档位（120/160/240/320/480/640/740/960/1080/1280/1600/1920/2560），避免任意宽度把缓存目录撑爆。
- **外链图代抓 SSRF 修复**
  - 重定向目标同样校验主机白名单，白名单域名 302 到内网地址会被拒绝。
- **限流来源判定**
  - `ipHash` 只信任 OpenResty 写入的 `X-Real-IP`，移除 `chimw.RealIP`，避免伪造 `X-Forwarded-For` 绕过限流。
- **浏览量累加**
  - 异步更新改用独立 context（3s），请求结束不再中断 UPDATE。
- **公开读接口缓存**
  - `/api/v1/posts`、`/posts/{slug}`、`/comments`、`/terms`、`/settings`、`/themes`、`/plugins`、`/ext` 下发 `Cache-Control: public, max-age=30, stale-while-revalidate=120` 并输出 ETag，命中 `If-None-Match` 返回 304。
- **`/sitemap.xml`**
  - 由后端按库中已发布文章实时生成（含 lastmod），OpenResty 已反代。
- **可观测性**
  - 新增访问日志（method/path/status/耗时/IP，静态图 2xx 略过）；`/healthz` 增加 2s 数据库探活，异常返回 503。
- **安全头**
  - API/图片响应增加 CSP（`default-src 'none'` 等）。
- **性能**
  - 鉴权中间件的 `pwd_changed_at` 查询加 30 秒内存缓存，改密/重置时主动失效。
  - AI 调用增加 120 秒整体超时，避免失败转移串行重试无限拖长。
  - 评论父级校验改为单条 `EXISTS` 查询。

### 数据库

- 迁移文件名去重：`0008_ai_connections.sql` → `0008a_ai_connections.sql`，`0008_s3_storage.sql` → `0008b_s3_storage.sql`（`schema_migrations` 已同步更新）。
- 删除历史备份表 `posts_bak_20261031`、`posts_html_bak_20261102`、`posts_html_bak_fanke_20261102`、`post_terms_bak_20261019`、`terms_bak_20261019`（删除前已 dump 到服务器 `/root/fanke-bak-tables-*.sql.gz`）。

### 前端

- `usePublicApi` 支持区分 404 与网络错误；请求改用浏览器缓存（后端已下发短缓存）。
- 公开读接口在已登录时附带 `Authorization`，用于草稿预览；移除已失效的「站内跳转」标记（`X-Fanke-Internal` + `plugins/internal-nav.client.ts`）。
- 文章页/首页补充 OG 与 Twitter Card 标签。
- 新增兜底路由 `pages/[...slug].vue`，未知路径展示 404 页（静态托管下状态码仍为 200，需 SSR 或边缘规则才能返回真 404）。

### 运维

- `fanke-api.service` 增加 `MemoryMax=512M`、`CPUQuota=200%`、`ProtectHome`、`PrivateDevices`、`RestrictAddressFamilies` 等加固项。

---

## 2026.10.31

### 前端

- **image-viewer 默认样式不生效（修复）**
  - 前台读取插件配置时，公开 hook `store.get_batch` 实际返回 `{"ok":true,"data":{"items":{…}}}`，旧代码按平铺的 `items` 解析，取到的默认 `des_sty` 永远为空：未自定义的图片没有任何默认样式，自定义了 `des_sty` 的图片也只剩标签里那几个 token。现改为读取 `data.items`（同时兼容旧的平铺结构）。
  - `des_sty` 与默认配置改为按 token 合并：标签写了哪些 token 就覆盖哪些，没写的（如 `fs`、`b`）继续沿用插件默认值。
- **页脚脚本定时器泄漏（修复）**
  - 页脚自定义脚本（如站点运行时长）内的 `setInterval` 在离开前台页面后仍在运行，`document.getElementById('site-uptime')` 已被卸载，控制台会周期性报 `Cannot set properties of null`。
  - 页脚脚本执行期间临时接管 `setInterval` / `setTimeout` 记录定时器 ID，页脚内容更新或组件卸载时统一清理，不再影响后台等页面。

### 插件

- `image-viewer` 升级到 **1.4**：修复默认配置读取与 `des_sty` 合并逻辑；插件详情页补充 1.4 更新记录，并说明「标签 token 覆盖默认值、未写的沿用默认」。

---

## 2026.10.29

### 后端

- **AI 连接器分组管理**
  - 新增 `POST /api/v1/admin/ai-connections/group-create`：新建空分组，名称写入 `settings.ai_groups`。
  - 新增 `POST /api/v1/admin/ai-connections/group-delete`：删除分组，`action=delete` 连同组内模型一起删除；`action=move` 把模型迁移到 `target` 分组，模型、密钥与权重全部保留。
  - 删除/重命名分组后自动修正 `settings.ai_selection`：迁移时改指目标分组；连带删除时改指剩余分组（没有剩余分组则清空）。
  - 分组列表 = 库中实际分组 ∪ `settings.ai_groups` 中的空分组。
- **写请求并发去重**
  - 新增中间件 `dedupeWrites`：管理端 `POST/PUT/PATCH/DELETE` 按「用户 ID + 方法 + 路径」判断，同一请求未完成前重复到达直接返回 `409「请求正在处理,请勿重复提交」`。
  - 只做后端兜底；前端原有的全屏忙碌遮罩保持不变。
- **AI 凭据复用与脱敏**
  - 所有 AI 连接相关响应（列表 / 详情 / 新建 / 更新 / 批量）中的 `api_key` 与 `api_url` 在服务端读取时即脱敏：密钥只保留末 4 位，URL 的 userinfo 与 `key/token/secret/password` 类查询参数被遮住，明文密钥不再返回浏览器。
  - 向已有分组添加模型时可只提交分组名与模型列表：后端复用该分组最近一条连接的 Provider / API 地址 / 密钥（新增 `GroupCredentials`），`POST /admin/ai-models` 同样支持只传 `group` 查询模型列表。
  - 编辑连接时密钥留空表示不修改，避免把脱敏串写回数据库。
- **AI 负载均衡失败转移**
  - 同组多连接不再只挑一条：按权重不放回抽样得到尝试顺序，逐个调用，任一成功即返回；只有全部失败才报错，错误里带上每条连接的分组/模型与上游原因。
  - 整体超时或调用方取消时立即停止，不再继续尝试后续模型。
- **插件通知能力 `notify.send`**
  - 新增 hook：插件申请后可通过 `Fanke.callHook('notify.send', {...})` 让宿主显示通知。
  - `scope=global` 右上角通知（ElNotification）、`inline` 插件区域内提示（宿主渲染可关闭的 el-alert）、`modal` 弹出框（ElMessageBox）。
  - 后端只做参数校验与默认值补全，渲染由编辑器面板与插件详情页的桥接完成。
- **hook 调用限流**
  - 所有 hook 按「插件 + hook」每分钟限流，超限返回 `429` 并带 `Retry-After: 60`；`ai.chat` 20 次/分钟、`store.import` 12、`http.check_url` 30 等，未单独配置默认 60。
  - 管理端与公开通道共用同一个桶，防止用公开接口绕过限制。
- 默认设置 `theme` 的内置主题名由 `material` 改为 `element-plus`。

### 前端

- **全局统一 Element Plus 风格**
  - 后台侧栏图标由 emoji/字符改为 `@element-plus/icons-vue` 组件（仪表盘、文章、媒体、评论、插件、AI 连接器、用户、设置）。
  - 原生 `button` / `checkbox` / `table` 改为 `el-button` / `el-checkbox` / `el-table`（用户管理、文章列表、媒体库、编辑器浮动按钮、主题切换、移动端菜单按钮）。
  - 原生 `confirm()` / `prompt()` 全部替换为 `ElMessageBox.confirm` / `ElMessageBox.prompt`（媒体库、用户、文章、评论、插件、AI 连接）。
  - `ElMessageBox` 消息文本支持换行（`pre-line`），多行警告保持可读。
  - 插件注册的编辑器面板按钮去掉 `size="small"`，与工具栏其它按钮同尺寸。
- **仪表盘按角色分流**
  - 管理员：问候 + 统计 + 快捷入口。
  - 非管理员：只显示「个人资料」与「修改密码」卡片，且侧栏隐藏插件 / AI 连接器 / 用户 / 设置入口。
  - 个人资料与密码修改只在「用户管理」里由管理员操作。
- **分类 / 标签页**
  - 详情页顶部切换胶囊与总览页字号、内边距完全一致（15px / 6px 16px），点击切换不再改变文字大小。
  - 当前项高亮保留。
  - 详情页内切换标签/分类不再重挂载页面（路由 `meta.key` 固定），标题、胶囊与 header 原地更新，只有下方文章列表刷新；连点切换会丢弃过期响应，切换术语时保持滚动位置，仅在翻页时回到顶部。
- **搜索改为浮层**
  - 导航里的「搜索」不再跳页，点击后弹出悬浮搜索框；`Ctrl/⌘ + K` 可随时打开，`Esc` 关闭。
  - 输入关键字（220ms 防抖）后，下方展示相关性靠前的 6 篇文章卡片，命中词高亮，点击卡片直接进入文章。
  - 卡片左侧显示文章封面（无封面时占位图标），标题单行截断并保留完整 `title` 提示。
  - 交互优化：输入框改为圆角（999px），查询中显示旋转图标且保留上一次结果不闪烁；`↑/↓` 切换高亮项（鼠标悬停同步）、`↵` 打开当前项、`Esc` 关闭；底部显示快捷键提示与结果总数；打开时锁定页面滚动并自动全选关键词，关闭后保留上次关键词。
  - 回车打开第一篇结果；「查看全部 N 条结果」进入 `/search` 完整结果页，该页仅保留输入框与分页列表。
- **文章列表布局重做**
  - 「状态 / 缩略图 / 标题」合并为一列：左侧 64×40 封面（点击放大），右侧标题 + slug + 状态标签，单行截断不撑高。
  - 「公开 / 置顶」从挤在操作区的复选框改为独立列的表头带说明的开关；操作列改为右对齐的文字按钮（编辑 / 发布或查看 / 删除），尺寸与间距统一。
- **主题文案**
  - 设置页主题描述由 "Material Design" 改为「内置 Element Plus 主题」。
- **AI 连接器页**
  - 新增「新建分组」入口；分组卡片新增「添加模型 / 重命名 / 删除分组」。
  - 分组卡片显示已保存的 API 地址与脱敏密钥。
  - 批量添加模型时，若分组已有连接，默认复用已保存凭据（可切换为重新填写）；「获取模型列表」也走复用，不再需要重复输入密钥。
  - 删除分组弹窗二选一：一并删除组内模型，或迁移到已有分组。
  - 重命名由浏览器 `prompt` 改为 EP 对话框；「当前使用」的分组选择去掉随意输入（分组必须先存在）。

### 插件

- `image-viewer` 升级到 **1.3**：默认说明位置/样式改为渲染时套用插件配置，修改默认值后已发布文章自动生效；后端保存时只补 `</img>` 闭合，不再写死 `des_pos="middle bottom"`；历史自动写入的默认位置已清理（备份表 `posts_bak_20261031`）。
- `seo-optimizer` 升级到 **1.5**：AI 分析/润色失败时通过 `notify.send` 弹框列出每条连接的分组/模型与失败原因；申请通知能力。
- `seo-optimizer` 升级到 **1.4**：新增「AI 润色」——对编辑器选中段落生成 2 个候选，差异对比（绿新增/红删除）确认后替换选中内容，支持撤销；未选中时提示先选段落，单次上限 4000 字。
- 编辑器面板消息扩充：宿主推送内容时附带 `selection`（当前选中纯文本）；插件可用 `content_text` 替换选中内容、用 `fanke:editor:undo` 撤销（走 TinyMCE 历史栈）。
- `seo-optimizer` 升级到 **1.3**：点击「应用」后不再清空 AI 分析结果，可继续应用标题/摘要等多条建议；已应用项按钮变「撤销」，支持单条撤销与「全部撤销」；编辑器内容变化（含宿主回推）不再重置面板。
- 宿主 `fanke:editor:apply` 新增 `allow_empty` 标记，供插件撤销时回填空串；普通应用仍只接受非空值。
- 插件更新记录随 `info.json` 版本写入数据库 `extensions_data`，插件详情页可查。
- `seo-optimizer` **1.2**：修复编辑器面板入口不显示；AI 分析走通用 `ai.chat`；详情页新增更新记录。
- 所有插件均为前端页面 + 通用 hook，后端没有内置任何 SEO / 业务提示词逻辑。

---

## 插件更新记录索引

| 插件 | 版本 | 位置 |
| --- | --- | --- |
| SEO 优化器 `seo-optimizer` | 1.0 → 1.2 | 插件详情页「更新记录」 |
| 更好用的 `<img>` 标签 `image-viewer` | 1.0 → 1.2 | `doc/plugins.md` 第六节 7、8 小节 |
| 图片焦点选择器 `image-focus` | 1.0 | 插件详情页 |
| 图片处理 `image-processing` | 1.0 → 1.3 | 插件详情页 |
| S3 对象存储 `s3-storage` | 1.0 → 1.3 | 插件详情页 |
