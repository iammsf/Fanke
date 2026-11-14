# S3 对象存储

为 Fanke 提供 S3 兼容的多桶对象存储：在后台集中管理桶别名、凭证和默认桶，媒体库按别名上传，文件保存在远端桶中。

## 功能

- 多桶管理：每个桶可单独启用、停用、移除，并单独填写 Endpoint、Region、桶名、公开访问地址、路径前缀和凭证。
- 默认桶：任意数量的启用桶中必须指定一个默认桶，上传菜单会把默认桶排在前面。
- 上传菜单：宿主为每个已保存的桶生成「上传至<别名>」菜单项，存储标识为 `s3:<桶ID>`；「本地」始终保留。禁用或配置不全的桶仍可能出现在贡献列表中，但选择后会被宿主拒绝上传。
- 保存对象：插件服务端把宿主提供的临时文件 `PutObject` 到远端桶，写入 MIME 类型与 `Cache-Control: public, max-age=31536000, immutable`，并返回 `public_base + "/" + key` 作为访问地址。
- 读取对象：`storage.open` 从远端下载到宿主临时文件，由宿主读取后再删除临时文件。
- 删除对象：`storage.delete` 删除远端对象；移除桶配置本身不会删除任何远端对象。
- 桶列表：`storage.listBuckets` 调用 S3 `ListBuckets`，结果填入桶名称输入框的候选列表。
- 读写测试：`storage.test` 先写入 `.fanke-probe-<纳秒时间戳>`，再立即删除；成功即视为读写可用。
- 媒体同步辅助：`storage.list`、`storage.head`、`storage.setCacheControl` 供宿主检查对象元数据、补写缓存头等同步流程使用。
- 与图片处理协作：上传时宿主先执行 `beforeMediaUpload` 生命周期事件，再执行图片处理插件生成派生图，最后才调用本插件保存；顺序固定为「图片处理 → 存储」。
- 上传 key 由宿主生成：S3 目标会使用配置里的对象路径前缀，默认按 `前缀 + sha256 前 16 位 + 扩展名` 直放；本地存储仍按年月目录。
- 插件停用后不再提供上传菜单，也无法进行新的 S3 上传，但已有远端对象和媒体记录不受影响。

## 安装

- 后台「插件」页上传 `.zip` 安装包，安装后进入详情页点击初始化，再到插件列表启用。
- 安装包可由 `deploy/install.sh` 生成：脚本会编译 `server/bin/linux-<arch>/s3-storage-server`，再把整个目录打包到 `/opt/fanke/dist/extensions/s3-storage.zip`（加 `--skip-plugins` 可跳过这一步）。
- 运行的是独立 Go 子进程，宿主按 `entrypoint` 启动：包内必须存在与宿主 GOOS/GOARCH 匹配的 `server/bin/<goos>-<goarch>/s3-storage-server`。
- 子进程无外部命令依赖；宿主每次调用启动一次进程，通过 stdio 传入一条 JSON 请求并读取一条响应，默认超时 30 秒。
- 服务器需要能访问所填写的 Endpoint（通常是 HTTPS）；不需要公开暴露插件目录。
- 首次启用会弹出「启用可信程序」确认，因为该插件以博客服务账号运行。

## 配置

在插件详情页编辑，字段以每个桶为单元：

| 字段 | 默认值 | 校验与说明 |
| --- | --- | --- |
| 启用此桶 | 新增时为开 | 关闭后不能作为默认桶或上传目标；但菜单贡献仍可能列出该桶 |
| 上传菜单别名 | 空 | 启用时必填，用于生成「上传至<别名>」 |
| Endpoint | 空 | 启用时必填，S3 API 地址，例如 `https://s3.example.com` |
| 区域 Region | `auto` | 为空时服务端按 `auto` 处理 |
| 存储桶名称 | 空 | 启用时必填，可点「获取桶列表」后从候选中选择 |
| 公开访问地址 | 空 | 启用时必填，用于拼接对象 URL，末尾斜杠会被去掉 |
| 对象路径前缀 | 空 | 可选，例如 `blog/`；作为上传对象目录前缀 |
| Access Key ID | 空 | 启用时服务端要求非空；密码框，留空表示保留 |
| Secret Access Key | 空 | 启用时服务端要求非空；密码框，留空表示保留 |
| Session Token | 空 | 可选，仅临时凭证需要；留空表示保留 |
| force_path_style | 勾选 | 勾选使用路径式访问；服务端在字段缺省时也按 `true` 处理 |
| 默认 S3 桶 | 无 | 单个单选；只有启用的桶可选，有启用桶时必须选中一个 |

- 保存前页面会校验：所有启用桶必须有别名、Endpoint、桶名称和公开访问地址；并且默认桶必须是已启用的桶。
- 宿主后端还会校验：启用桶必须有 Endpoint、Access Key、Secret Key 和桶名；默认桶 ID 必须存在；桶 ID 不可重复。
- 点击「获取桶列表」或「测试读写」使用的是当前表单值，不必先保存；测试成功只代表临时探针对象写入并删除成功。
- 保存成功后会自动重新读取配置并刷新上传菜单；「重新读取」会丢弃未保存修改。

## 声明的能力与 Hook

`info.json` 中 `capabilities` 是子进程实现的 JSON-RPC 方法，`request` 是向宿主申请的前端/生命周期 Hook：

| 类型 | 名称 | 用途 |
| --- | --- | --- |
| capability | `storage.save` | 保存对象到指定桶，返回 key、URL、MIME、大小 |
| capability | `storage.open` | 下载对象到指定临时路径 |
| capability | `storage.delete` | 删除指定 key 的远端对象 |
| capability | `storage.list` | 分页列出对象，单次最多 10000 条 |
| capability | `storage.head` | 读取对象大小、MIME、Cache-Control |
| capability | `storage.setCacheControl` | 通过复制对象重写 Cache-Control，保留原有元数据 |
| capability | `storage.listBuckets` | 获取当前凭证可见的桶名称列表 |
| capability | `storage.test` | 写入并删除探针对象，验证读写 |
| request | `beforeMediaUpload` | 订阅媒体上传生命周期；宿主据此把本插件视为可用的存储提供者 |
| request | `addSettingsItems` | 在后台设置页贡献「S3 对象存储」配置入口（链接到插件详情页） |
| request | `addUploadMediaMenuItem` | 允许在媒体上传菜单中按桶生成「上传至<别名>」项 |

## 数据存储

- 桶配置保存在 `settings` 表的 `s3_buckets` 键，JSON 结构为 `{ "buckets": [...], "default_bucket_id": "..." }`。
- 桶 ID 在未显式提供时由 `endpoint|region|bucket|public_base` 的哈希派生，形如 `s3-xxxxxxxxxxxxxxxx`；因此改动这些字段会被视为另一个桶。
- 兼容旧版单桶配置：读取时若没有 `s3_buckets`，会回退读取 `s3_config` 并转换为多桶结构。
- Access Key、Secret Key、Session Token 在写入数据库前加密，读取时解密；接口返回配置时 Secret Key 和 Session Token 会被掩码为 `********`，页面也不会回显三项凭证。
- 保存时若凭证为空或等于掩码，宿主会沿用该桶原有的凭证；新桶留空则视为未配置。
- 媒体表只记录存储标识（`s3` 或 `s3:<桶ID>`）、对象 key 和 URL；移除桶配置不会删除远端对象。
- 如果某个桶已被媒体或派生图引用，删除该桶或改变其 Endpoint/Region/桶名/公开地址会被拒绝，避免历史链接失效。

## 使用方式

- 在后台「插件」页上传安装包，进入详情页完成初始化并启用。
- 在插件详情页点「添加存储桶」，填写配置并保存；可用「获取桶列表」选择桶，用「测试读写」验证凭证。
- 在媒体库或编辑器上传入口打开上传目标菜单，选择「本地」或「上传至<别名>」；默认桶排在最前。
- 在后台「设置」页可以看到插件贡献的「S3 对象存储」卡片，点击「打开插件配置」回到详情页。
- 修改桶的公开访问地址后，新上传对象使用新地址；历史对象的记录不会被自动改写。

## 安全与限制

- S3 凭证属于敏感信息，只应填写在后台插件详情页；不要写入仓库、README、截图或提交记录。
- 建议使用 HTTPS Endpoint 和最小权限凭证（仅允许目标桶的 Get/Put/Delete/List）；Session Token 仅用于临时凭证。
- `public_base` 必须指向可公开读取该桶的地址；插件返回的是普通拼接 URL，不做签名，桶或 CDN 需自行配置公开读。
- 新保存的对象固定写入一年 immutable 缓存头；需要修正已有对象缓存头时可使用 `storage.setCacheControl`。
- 上传失败会直接返回错误（HTTP 502），不会自动回退到本地存储；请先确认 Endpoint、桶策略和凭证。
- 宿主上传限制：图片和其他附件单个最大 100MiB，视频最大 1GiB；仅允许白名单扩展名，且桶内容策略（`allowed`）还会再次过滤。
- 桶内容策略 `allowed` 未在插件页提供修改入口，保存配置时也不会提交该字段，因此实际按默认值处理：空值等同 `images`，S3 只接受 jpg/jpeg/png/gif/webp/svg/avif，视频和文档会被拒绝。
- 单次上传会先执行图片处理；图片处理失败时原图保留并给出警告，S3 保存失败则本次上传整体失败。
- 相同 SHA256 只在同一目标存储内复用；选择其他桶时会真实上传并建立新记录。
- 子进程单次调用默认 30 秒超时、响应上限 4MiB；超大文件或极慢网络可能导致上传失败。
- 移除桶配置或停用插件都不会清理远端对象，删除文件需在媒体库或对象存储控制台操作。

## 关于

- 作者：電腦玩家Stan
- 作者主页：https://boke.masi.fan
- 插件主页与更新通道：暂未提供。
