# fanke 更新记录

本文件记录系统本身的变更（后端 / 前端 / 文档）。插件各自的更新记录写在插件详情页，见文末索引。

---

## 2026.11.17

### 插件

- **image-viewer 1.7：向核心登记 `<fanke-img>` 的回落模板**
  - `info.json` 新增 `fallback` 字段，声明插件停用/卸载/文件损坏时正文里 `<fanke-img>` 的渲染方式（`figure` + `img` + `figcaption`，占位符 `{{src}}` / `{{caption}}`）。
  - 此前插件一旦不在线，已发布文章里的 `<fanke-img>` 会退化成浏览器不认识的裸标签（既不显示图也不显示图注），编辑器里更是直接暴露原始标签文本。
  - 账本在**安装/更新插件时**登记，所以已装 1.6 的站点要升级到 1.7 才有回落（后台插件页会出现更新提示）。
- **image-viewer 1.6：修复可视化编辑器里直接改图注不入库**
  - 画布上 `figcaption` 的文字现在是图注的唯一权威来源。此前 `editor.ts` 的 `fromEditor` / `read` 优先读画布 `figure` 上的 `des` **属性**，而用户改的是 `figcaption` 的**文字**，保存时新文字被旧属性覆盖，写回数据库的仍是旧图注；取值顺序现已与文章页 `useImageViewer.ts` 对齐。
  - `render()` 改为始终输出 `figcaption`（`des_sty` 含 `hidden` 时用 `display:none`），且不再往画布 `figure` 写 `des`：清空图注会真的存成空、隐藏图注不再丢失、空图注也能直接点进去输入。
  - 顺带修复历史内容丢图注：图注写在 `des` 属性里的老文章，此前一进编辑器就被清空，保存后库里也没了。

### 后端

- **新增通用「标签回落账本」**（`backend/internal/app/extensions_fallback.go`）：核心不再为任何具体插件写死回落分支，插件在 `info.json` 声明 `fallback`（标签 → HTML 模板），安装/更新时由核心消毒后记进自己的 settings（键 `tag_fallbacks`）。
  - **卸载插件不删账本**：正文里已写入的标签是历史事实，必须永远能渲染。
  - 模板在安装时用哨兵 URL 占位后过 `render.SanitizeHTML`（script/事件属性/`javascript:` 直接被剔掉），`{{占位符}}` 不会被 URL 校验百分号编码毁掉；模板 ≤2KB，标签名须匹配 `^[a-z][a-z0-9-]{0,47}$`。
  - **标签所有权唯一**：别的插件已登记的标签再声明会被拒绝（400）；更新时先清掉本插件旧登记，新版本不再声明的标签自动移除；登记写库失败会回滚整个安装（删目录 + 删记录）。
  - `GET /api/v1/ext` 的 `enabled` 口径改为「已启用 + 已初始化 + **插件文件完好**」（详情页 `index.html` 必须在；声明了 `editor.register` 的必须有 `editor.js`/`editor.ts`），文件损坏的插件列进新增的 `unhealthy`，并随 `fallbacks` 一起下发给渲染端。这样插件文件被删/损坏也等同「不在线」，自动走回落，而不是让编辑器去加载一个 404 的脚本。
  - 覆盖上传安装与在线升级两条路径（`handlers_extensions.go` / `handlers_extensions_update.go`）。
- **多桶上传写库失败**：`media.storage` / `media_variants.storage` 的 CHECK 只允许 `'s3'`/`'local'`，而多桶上传写的是 `'s3:<桶ID>'`。在未手改过约束的库上插入会失败，并连带回删刚上传的对象与派生图。新增 `0010_media_storage_bucket.sql` 放宽为 `local | s3 | s3:%`。
- **删除 s3-storage 的保护失效**：`HasS3MediaReferences` 只匹配 `storage='s3'`，多桶记录全被漏掉，导致仍有媒体引用时也能把插件删掉。
- **热接入数据库后 AI 接口一直 500**：`attachDB` 没有重建 `ai.Client`，它仍持有启动时的 nil DB；引导/找回模式下配置完数据库后，`ai.chat` 与后台 AI 接口会 nil 解引用 panic，直到重启进程才恢复。
- **插件升级后编辑器仍跑旧代码**：插件静态文件一律发 `Cache-Control: public, max-age=3600`，而插件详情页 `index.html` 是 `no-store`。升级后一小时内浏览器直接复用缓存里的旧 `editor.js`，后台版本号已是新版、编辑器行为还是旧版（image-viewer 1.6 的图注修复装完仍「不落库」就是这个原因）。现在代码类文件（js/mjs/cjs/ts/css/html/json/map）改为 `no-cache`，靠 `Last-Modified` 回源校验，内容没变仍是 304；图片等二进制保留一小时缓存。
- `go test ./...` 不再因缺少 `extensions/s3-storage/server/bin/` 构建产物而整体失败：该用例改为跳过并打印构建命令。
- **管理页批量操作 API**（`backend/internal/app/handlers_bulk.go`）：`POST /admin/{posts,comments,users,extensions}/bulk`，统一 `{action, ids|slugs, …}` 入参、动作白名单、逐项容错返回 `{ok, failed[]}`，单次上限 500 项。
  - 纯 SQL 动作（发布/公开/置顶/评论状态）一条 `WHERE id = ANY($1)` 打完，不再逐条往返；带生命周期钩子的动作（删文章）仍逐条走，保住 `BeforePostDelete` 的否决权。
  - 批量删用户前先一次查清 `posts.author_id` / `comments.user_id` 引用（两者都是 `NO ACTION` 外键，直接删会 500），翻译成「仍有文章，请先转移作者」这类可读原因。
  - 批量置顶接着当前最大权重按勾选顺序往后排，与单条 `SetPostSticky` 的「排到队尾」语义一致。
  - 顺手把三处与单条接口重复的逻辑抽成共享实现（删文章、启用/停用插件、卸载插件），并用 `rejectedError` 区分「业务规则拒绝」（422/409）与「基础设施故障」（500）——此前钩子否决会被报成 500。
- **热路径缺索引**：新增 `0011_hot_path_indexes.sql`，补 `media(url)`、`media(key)`、`post_terms(term_id)`、`comments(status, created_at DESC)` 四个索引，分别对应媒体引用统计、媒体库对账、文章页分类标签、后台评论待审列表四条实际查询。
- **媒体库对账从作者组移到管理员组**：`/admin/media/sync/{scan,apply}` 能列整个桶、扫全表引用、删桶内对象，属于基础设施级操作；放在作者组时任何作者点一下就能触发一次全桶 `ListObjects` + 全表 `position()` 扫描。
- **作者可以改删任何人的文章（横向越权）**：路由分组只做了角色门槛，`requireRole("author")` 的实际语义是「admin 或 author 都放行」，归属从来没校验过——任何作者拿到别人的文章 id 就能改、删、发布、切公开、置顶。现在按「管理员管理所有文章，作者只能动自己的文章」落地：新增 `canEditPost` / `requireOwnPost`（`handlers_posts_authz.go`），接入全部写路径（`UpdatePost` / `DeletePost` / `PublishPost` / `SetPostPublic` / `SetPostSticky` / `SetStickyOrder` / `BulkPosts` / `GetPostAdmin`），非本人文章一律 403「只能操作自己的文章」。
  - 批量接口保持**逐项容错**：别人的 id 跳过并记进 `failed[]`，不整批拒绝；归属过滤在 `deletePostWithHooks` 之前完成（该函数被单条删除共用，自身不校验归属）。
  - 后台文章列表对作者按 `author_id` 收窄（`repo.PostQuery.AuthorID`，0 表示不过滤）。该字段只在 `ListPostsAdmin` 一处赋值，`parsePostQuery` 与 `renderPostList` 是公开列表共用的，因此 `GET /posts`、首页、分类、标签、搜索全部不受影响（已逐个调用点核对）。
  - 顺带修正：`publish` / `public` / `sticky` 单条接口对不存在的 id 从「静默 204」变成 404——归属校验必须先查库，查不到就不该报成功。
  - `CreatePost` 一直用 `currentUser(r).ID` 作作者、`UpdatePost` 一直沿用 `old.AuthorID`，所以「把文章挂到别人名下」或「改归属」从来做不到，这两处未改。
- **置顶排序能抹掉全站他人的置顶**：`SetStickyOrder` 的第一条 SQL 是 `UPDATE posts SET sticky=0 WHERE sticky>0 AND NOT (id = ANY($1))`——不分作者地把所有不在传入列表里的置顶取消掉。作者拿自己的 token 只提交自己的文章 id，能通过上面新加的归属校验（每个 id 确实都是他的），然后把管理员和其他作者的置顶全部抹掉。UI 上点不出来，API 直接可达。
  - `posts.sticky` 是**全站共用的整数序号**（公开列表按它排序），「未包含者一律取消置顶」的语义天然是全局的，所以把重排全站置顶顺序认定为站点级操作：`PUT /admin/posts-sticky` 移入管理员组。作者仍可通过 `POST /admin/posts/{id}/sticky` 置顶/取消置顶自己的文章（沿用「排到队尾」语义）。
  - `GET /admin/posts-sticky` 留在作者组但按归属收窄（`repo.ListStickyPosts(ctx, authorID)`，0 表示不过滤），作者面板只显示「我置顶的文章」。
  - 前端：非管理员隐藏「保存顺序」按钮与 ⠿ 拖拽把手，`li` 的 `draggable` 绑定为 `'false'`（枚举属性，浏览器不发起 dragstart），`dragstart`/`dragover` 处理器再兜底一层；标题与空态文案按角色区分，并说明顺序由管理员统一安排。
- **媒体删除/转移与分类标签删改收归管理员**：媒体是**全站共享池**（`media` 表没有归属列，任何作者上传的图都能被别人引用进正文），所以删除或转移到 S3 会直接破坏他人文章的配图，不是「动自己的东西」。按此把 `DELETE /admin/media/{id}`、`POST /admin/media/batch-delete`、`POST /admin/media/batch-transfer-s3` 移入管理员组；上传、列表、批量打包下载留在作者组（这三者都不改动他人数据）。
  - 分类标签同理：`PUT /admin/terms/{id}`、`DELETE /admin/terms/{id}` 移入管理员组——改名/删除会连带改掉所有引用它的文章的分类展示。`POST /admin/terms` 留在作者组，作者写文章时仍能新建分类/标签。
  - 前端媒体库对非管理员隐藏破坏性入口：批量条里的「删除」「转移到 S3」、卡片上的「删除」按钮、「同步 S3」按钮（复用同一个 `isAdmin`，不重复请求 `me`）。「下载」与「取消选择」不受限。
- **对账接口的 N+1 查询**：`SyncScan` 此前对每个孤儿对象/悬空记录各发一次引用统计（5000 个对象就是 5000 条 SQL），改为 `unnest` 批量一次查完；错误也不再被 `refs, _ :=` 静默吞掉。批量口径只用于**展示**，`SyncApply` 删除前仍逐条用原始单条查询复核，统计出错不可能变成误删。
- **每次图片请求都读库解密 S3 配置**：`thumbSource` / `mediaStorage` 是热路径，而 `repo.S3Settings` 每次都要读库并对每个桶做一次 AES-GCM 解密。新增 `App.s3Settings` 快照缓存，挂在 `publicCache` 的换代号上——配置一改自动失效，不需要单独的失效路径。
- **改 S3 配置后最长 30 秒不生效**：`PutS3Config` 既不清 `publicCache` 也不清 `s3Stores`，新桶配置要等公开缓存自然过期才被看见。
- **缩图并发合并会无限期挂住请求**：`singleflight` 的等待没有带 `ctx`，领头那个若卡在 `image.process` 子进程上，同 key 的后续请求全都放不掉（客户端早断开了也照样挂着）。
- **`ip_hash` 用固定盐等于把算法公开**：拖库之后可以离线把整个 IPv4 空间跑一遍反查评论者真实 IP。改用站点自己的 `JWT_SECRET` 做 HMAC；`ip_hash` 没有任何读取方（模型上是 `json:"-"`），换算法不影响功能，只是历史值对不上新值。
- **`sitemap.xml` 的域名来自请求头**：Host 可被伪造，会往站点地图里写进别人的域名（缓存投毒）。新增 `SITE_URL` 配置，优先用它，没有才回落 Host。
- **更新下载的 curl 通道会跟随未校验的重定向**：`curl -fsSL` 的 `-L` 无条件跟 3xx，而 `validProbeURL` 只校验了最初那个地址——一条外部 https 地址就能把请求引到内网或云元数据（SSRF）。改成不带 `-L` 手动逐跳，每跳都过 `validProbeURL`，跳数上限与已加固的 Go 探测路径一致（4 跳）。curl 通道本身保留：国内服务器直连 GitHub 常超时，它存在的意义就是能复用系统代理。
  - 顺带修掉一个 Windows 专属故障：Windows 版 curl 的 `-w` 输出是 CRLF，`Atoi("302\r")` 得 0，更新下载会被判成「返回 0」而全线失败。
- **插件包超限报的是 400**：`maxExtPackage` 超限现在回 413（`Request Entity Too Large`），前端能据此区分「包太大」和「参数错了」。
- **回落账本登记存在读-改-写竞态**：两个插件同时安装会互相覆盖 `tag_fallbacks`。新增 `repo.MutateSettingLocked`（确保行存在 → `FOR UPDATE` 行锁 → 读 → 改 → 写，单事务），并在动文件系统之前先校验声明，校验不过不落盘。

### 前端 / 文档

- **插件标签自动回落**（`utils/tagFallback.ts` + `composables/useTagFallbacks.ts`）：按后端下发的账本渲染，核心里没有针对具体插件的分支。
  - 文章页：`iv.transform(fb.render(content_html))`——插件在线时仍走 image-viewer 的完整渲染（`des_sty` 令牌合并 + 插件默认配置），不在线时按账本模板出图与图注；新增 `.fanke-fallback` 中性排版，任何插件的回落结果都不会撑破正文。
  - 后台编辑器：不在线插件的标签包成信封 `<div class="fanke-fallback-box" contenteditable="false" data-fanke-fallback data-fanke-orig="原样落库标签">模板视觉</div>`，保存时拆信原样还原，停用期间编辑正文不丢标签。信封整体锁定是刻意的：里面的视觉只是替身，改它写不回落库标签，与其悄悄丢编辑不如锁住（暖色虚线框 + 悬停提示），插件恢复后自动解锁。
  - 右键「编辑 HTML」与「工具 → 源代码」照旧只展示/接收落库形态，因此插件停用期间仍能手改标签本身。
- **三处 `/ext` 请求合并成一处**：`useExtensions`、`useEditorRegistry`、`useTagFallbacks` 此前各自 `fetch('/ext')`，现在统一由 `useExtensions.refreshPublic()` 一次拉齐 `enabled` / `editors` / `fallbacks`（带 in-flight 去重），后两者只读共享状态。
- **四个管理页支持批量选中与批量操作**（文章、评论、用户、插件）：此前这些列表只能一条一条点，删 100 条垃圾评论要点 100 次。
  - 文章：发布 / 设为公开 / 取消公开 / 置顶 / 取消置顶 / 删除；置顶后重新拉一次权重列表，取消置顶只作用于真的在置顶的项。
  - 评论：通过 / 待审 / 垃圾 / 删除，用卡片上的复选框多选，切标签页与翻页都会清空选择（避免把看不见的项一起提交）。
  - 用户：批量删除，当前登录用户那一行不可勾选。
  - 插件：批量启用/停用、批量卸载；选中项含 s3-storage 时沿用单条操作的「启用可信程序」二次确认，卸载确认文案会说明回落账本仍在（卸载后正文里的插件标签按安装时登记的模板继续渲染，不会丢内容）。
  - 搜索、切状态、翻页都清空选择，批量完成后只刷新受影响的局部数据。
- **核心不再为具体插件写死回落分支**：`useTagFallbacks` 里那张 `CORE_RENDERERS = { 'image-viewer': true }` 硬编码表换成 `useCoreTagRenderers()` 认领表——谁渲染某个落库标签，就由谁在 setup 期 `claim(tag)`（`useImageViewer` 认领 `fanke-img`）。以后任何插件的核心集成代码自己认领即可，不需要回头改回落逻辑。用 `useState` 而不是模块级变量：prerender 时模块状态会跨路由残留。
- **媒体库「同步 S3」入口只对管理员显示**：接口已移入管理员组，作者点进去只会吃一个 403。
- 后台首页「立即升级」按钮此前与「检查更新」共用 `Refresh` 图标，改为 `Top`（升级=往上走，检查更新=转圈），三个按钮图标不再撞车。
- **HTML 编辑界面回到落库标签形态**：右键「编辑 HTML」的代码块与「工具 → 源代码」此前直接暴露画布内部的 `<figure data-fanke-node=…><img><figcaption>` 包装，手写/改 HTML 又长又难读。现在两个界面都只展示与接收 `<img …>` / `<fanke-img …>说明</fanke-img>`，应用时再转回画布形态；「源代码」改由自绘对话框提供（去掉 TinyMCE code 插件），与内联编辑同一口径。开关语义不变：未勾选「替换默认 img 标签」时手打 `<img>` 保持原生、`<fanke-img>` 走插件，勾选后文章页两者都按插件渲染。
- 删除随旧编辑器预览一起失效的死代码：`useImageViewer.setTagSrc`、`transform` 的 `loose` 参数与 `.iv-editing` 样式。
- 删除未使用的 `marked` 依赖与 `usePublicApi.themeTokens`（旧主题的 `tokens.css` 由后端在 `site.css`/`admin.css` 接口内部回落，前端从未调用）。
- README 更正编辑器与主题格式的描述：编辑器是 TinyMCE 可视化 + 右键改单块 HTML（Markdown 只是导入历史内容的后端回落），主题当前格式是 `site.css` / `admin.css`。
- `doc/plugins.md` 新增「标签回落账本（`fallback`）」一节：触发条件（禁用/卸载/文件损坏）、占位符语法与取值顺序、消毒与所有权规则、文章页与编辑器两个渲染面的差异；并补 image-viewer 1.7 登记的实际模板与「回落态不生效的定制项」。顺手修好该文档里一处被写坏的 ```` ```js ```` 代码围栏。
- `doc/plugins.md` 补 Go 子进程插件的 `info.json` 字段（`protocol_version` / `trusted_process` / `entrypoint` / `capabilities` / `dependencies`）及其固定取值与构建命令；明确区分扩展包与历史遗留的内联 CSS/JS 片段系统（后者有接口无后台页面）；更新 image-viewer 的前端集成点描述。

### 运维

- **`deploy/nginx.conf.template` 补齐三处实际会踩的配置**（已部署的站点需手动同步，模板改动不会自动生效）：
  - **文本产物此前完全没压缩**：模板里没有 `gzip on`，`/api/` 的 JSON 与 JS/CSS 全部原样下发。补 5 级 gzip；`gzip_proxied any` 是必需的，nginx 默认不压缩反代响应，少了它 `/api/` 的 JSON 还是不会被压。
  - **大视频上传在 nginx 就被 413 掉**：`client_max_body_size` 只有 server 级的 `100m`，而后端 `maxUploadRequest` 是 1GiB + 1MiB。`location /api/` 单独放宽到 `1100m`，静态资源仍是 100m（那里不涉及上传）。
  - **发版后浏览器一直拿旧 `index.html`**：`location /` 此前没有任何 `Cache-Control`，SPA 外壳与预渲染 HTML 会被缓存，引用的还是旧哈希产物。改为 `no-cache`（「用前先问」，不是「不缓存」）；`try_files` 回落 `/index.html` 是内部跳转，会重新匹配到本 location，所以覆盖得到。
  - 顺带补：nginx 的 `add_header` 继承是**全有或全无**——`/_nuxt/` 一旦有自己的 `add_header`，server 级那三条安全头就一条都不继承，两个 location 都得重写一遍。
- **新增 `SITE_URL` 环境变量**：`sitemap.xml` 优先用它，不再从请求头取 Host（可被伪造）。不配则维持原行为。

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
