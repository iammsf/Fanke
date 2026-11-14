# Fanke

一个可自托管的个人博客系统：Go 后端 + Nuxt 3 纯静态前端 + PostgreSQL，单机即可跑起来。

- 文章：支持Markdown/HTML/可视化编辑、草稿/发布、置顶、公开/私密、定时发布时间
- 内容组织：分类、标签、关键词搜索公开文章
- 评论：访客评论与后台审核
- 媒体库：本地或 S3 兼容对象存储、图片焦点、WebP 派生图、批量下载/迁移、桶内对象对账
- 主题：上传 `tokens.css` 覆盖 CSS 变量，暗色模式
- 插件（扩展）：`.zip` 安装、声明式 hook 白名单、独立 Go 子进程承载存储/图片处理等能力
- 其他：安装向导、数据库断连找回、JWT 登录、AI 连接管理、SEO 检查面板

> 仓库中的默认配置不包含任何个人数据：默认管理员名为 `admin`，不绑定任何域名，插件作者信息统一为「電腦玩家Stan」。

---

## 目录结构

```
fanke/
├── backend/                Go API 服务（chi + pgx）
│   ├── cmd/api/            进程入口：加载配置 → 连库/迁移 → 启动 HTTP
│   └── internal/           app 路由与处理器 / auth / config / media / migrate / render / repo ...
├── frontend-nuxt/          Nuxt 3 前端（ssr:false，构建产物为纯静态 SPA）
├── extensions/             自有插件源码（每个插件一个目录，含各自 README.md）
├── deploy/                 一键部署脚本、systemd unit、nginx 模板、env 示例
├── doc/                    开发文档（hooks.md / plugins.md / changelog.md）
└── README.md
```

---

## 环境要求

- Linux（x86_64 / arm64），Debian/Ubuntu、RHEL/CentOS/Rocky/Alma/Fedora 均可
- root 权限（安装脚本会装依赖、建系统用户、写 systemd 与 nginx 配置）
- PostgreSQL 12+、nginx（可选，面板用户可用面板自带）
- 构建期需要 Go（版本以 `backend/go.mod` 为准）与 Node.js 18+；脚本会自动安装
- 服务器能访问 Go / Node.js 官方下载站与 npm registry

`deploy/install.sh` 会自动补齐：PostgreSQL、nginx、7z、cwebp、Go、Node.js、zip 等缺失依赖。

---

## 一键部署

```bash
git clone <仓库地址> fanke
cd fanke

# 推荐：交互式安装，逐项询问（直接回车用默认值）
sudo bash deploy/install.sh

# 全默认、不询问（适合自动化）
sudo bash deploy/install.sh --yes

# 非交互：指定域名
sudo bash deploy/install.sh --domain blog.example.com --yes

# 使用已有 PostgreSQL（跳过本地建库）
sudo bash deploy/install.sh --domain blog.example.com --yes \
  --db-url 'postgres://user:pass@127.0.0.1:5432/fanke?sslmode=disable'

# 更新代码后升级：保留数据与配置，重新构建并重启
sudo bash deploy/install.sh --update
```

交互式会依次询问：站点域名、安装目录、后端端口、数据库方式（本地 / 已有连接串）、是否现在创建管理员、是否配置 nginx、是否编译打包插件，最后给出配置摘要并确认。

脚本执行完后，浏览器打开 `http://<域名或服务器IP>`；如果交互时没有创建管理员，会进入安装向导（默认管理员名 `admin`）。

### 脚本做了什么

1. 识别发行版、CPU 架构，以及是否运行在宝塔 / 1Panel 面板上
2. 用 apt / dnf / yum 安装缺失依赖（PostgreSQL、nginx、7z、cwebp、zip 等）
3. 按 `go.mod` 要求的版本安装 Go，按需安装 Node.js 22
4. 启动 PostgreSQL，幂等创建数据库与账号（随机密码）
5. 编译后端到 `/opt/fanke/bin/api`，构建前端静态文件
6. 编译 `extensions/*/server` 子进程并打包成 `/opt/fanke/dist/extensions/<slug>.zip`
7. 生成 `/opt/fanke/state/fanke.env`（0600，含数据库连接、JWT 密钥、找回密钥）
8. 安装并启动 systemd 服务 `fanke-api`（含权限沙箱）
9. 写入 nginx 反代配置并重载，完成健康检查

重复执行是幂等的：已有配置文件与数据库会被保留，只重新构建并重启。

### 常用参数

| 参数 | 说明 |
| --- | --- |
| `--yes` / `-y` | 非交互模式，直接用参数与默认值安装 |
| `--domain <域名>` | 站点域名；不填则用 `_`，通过服务器 IP 访问 |
| `--dir <目录>` | 安装目录，默认 `/opt/fanke` |
| `--port <端口>` | 后端监听端口，默认 `8081`（只监听 `127.0.0.1`） |
| `--db-url <URL>` | 使用已有 PostgreSQL，跳过本地建库 |
| `--db-name / --db-user / --db-pass` | 本地建库参数，默认 `fanke` / `fanke` / 随机密码 |
| `--admin-user / --admin-email / --admin-pass` | 自动创建首个管理员（不填则走网页向导） |
| `--skip-nginx` | 只部署后端与静态文件，不改 nginx |
| `--skip-plugins` | 跳过插件编译与打包 |
| `--update` | 升级模式：保留配置与数据，重新构建并重启 |

### 面板适配

| 环境 | 脚本行为 |
| --- | --- |
| 纯净 Linux | 安装 nginx，写入 `/etc/nginx/conf.d/fanke.conf`，`systemctl reload nginx` |
| 宝塔面板 | 复用面板 nginx，站点目录 `/www/wwwroot/<域名>`，配置写入 `/www/server/panel/vhost/nginx/` |
| 1Panel | 静态文件放到 `/opt/1panel/www/sites/<域名>/index`，配置写入 OpenResty 的 `conf.d` 并尝试重载容器 |

1Panel / 宝塔写入配置后，如果站点未生效，在面板里重载一次 Web 服务即可；HTTPS 建议直接在面板中为站点申请证书。

### 部署后的目录

```
/opt/fanke/
├── bin/api                 后端可执行文件
├── state/fanke.env         运行时配置（0600，务必备份）
├── media/                  本地媒体卷
├── themes/                 运行时上传的主题
├── extensions/             服务端插件
├── web/                    前端静态文件（纯净 Linux / 宝塔场景）
└── dist/extensions/        编译好的插件安装包
```

---

## 安装向导与找回

- 首次访问：数据库已由脚本配好，页面直接进入「创建管理员」向导
- 数据库断连：站点进入引导模式，凭 `state/fanke.env` 里的 `RECOVERY_KEY` 可在线重配数据库
- 检查状态：`curl http://127.0.0.1:8081/healthz` 返回 `{"status":"ok","db":"ok"}`
- 忘记恢复密钥：直接编辑 `state/fanke.env` 后重启服务

---

## 版本与更新

- 后台仪表盘显示当前版本，管理员可点「检查更新」查询博客本体最新版本；有更新时出现「立即升级」
- 插件列表 / 插件详情页有各自的「检查更新」，显示远端版本、更新说明与发布页链接
- 检查逻辑读取仓库根目录的 [updates.json](updates.json)，默认走 GitHub raw：
  `https://raw.githubusercontent.com/iammsf/fanke/main/updates.json`
- 升级渠道可自定义，设置 `UPDATE_MANIFEST_URL` 即可切换，例如 jsDelivr
  `https://cdn.jsdelivr.net/gh/iammsf/fanke@main/updates.json`，或自建镜像（内容格式一致）
- 服务器直连 GitHub 超时（下载失败）时，设置 `UPDATE_PROXY` 走系统代理；后端优先用系统 `curl` 下载，兼容系统已有的代理配置

### 在线升级本体

管理页点「立即升级」后，后端会：

1. 从 GitHub Release 下载当前架构的 `fanke-<版本>-linux-<arch>.tar.gz`
2. 校验清单里的 `sha256`
3. 只替换 `bin/api` 与前端静态目录
4. 退出进程，由 systemd 拉起新二进制

`extensions/`、`themes/`、`media/`、`state/` 和数据库都不会动。要求服务账号对 `bin/` 与前端目录有写权限；systemd unit 已通过 `ReadWritePaths` 放行，安装脚本也会把前端目录 chown 给服务账号。

插件也支持在线更新：插件列表/详情页检查到新版本后会出现「更新」按钮，后端下载清单里的 zip、校验 `sha256`、替换插件目录，并保留插件数据表与启用状态。主题同理，在「设置 → 主题」里检查更新与更新。

也可以继续用部署脚本升级：

```bash
sudo bash deploy/install.sh --update
```

### 发布新版本

仓库带有 [release 工作流](.github/workflows/release.yml)，打标签即自动发布：

```bash
git tag v1.1.0
git push origin v1.1.0
```

工作流会：构建 linux amd64/arm64 后端与前端 → 打包全部插件 zip（含对应架构的 Go 子进程）→ 生成并提交 `updates.json` → 创建 GitHub Release 并附上产物。

首次使用前请在仓库 Settings → Actions → General 中把 Workflow permissions 设为 **Read and write**；若 `main` 有分支保护，需要放行 `github-actions[bot]` 的推送。

---

## 配置项

配置来源优先级：进程环境变量 > `/opt/fanke/state/fanke.env`。完整示例见 [deploy/fanke.env.example](deploy/fanke.env.example)。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `DATABASE_URL` | 空 | PostgreSQL 连接串；为空进入引导模式 |
| `JWT_SECRET` | 空 | 登录签名密钥；为空会生成临时密钥（重启后登录态失效） |
| `RECOVERY_KEY` | 空 | 断库找回口令 |
| `ADDR` | `:8080` | HTTP 监听地址；部署脚本写为 `127.0.0.1:8081` |
| `MEDIA_LOCAL_DIR` | `/data/media` | 本地媒体卷目录 |
| `MEDIA_BASE_URL` | `/uploads` | 本地媒体对外前缀 |
| `THEMES_DIR` | `/opt/fanke/themes` | 主题目录 |
| `EXTENSIONS_DIR` | `THEMES_DIR` 同级 `extensions` | 插件目录 |
| `THUMB_DIR` | 系统临时目录 | 缩略图缓存目录 |
| `THUMB_ALLOW_HOSTS` | 空 | 允许服务端代抓缩图的外部主机（逗号分隔，防 SSRF） |
| `REDIS_URL` | 空 | 可选，多实例共享限流计数 |
| `UPDATE_MANIFEST_URL` | GitHub raw 上的 `updates.json` | 更新清单地址；可换成 jsDelivr 或自建镜像 |
| `UPDATE_PROXY` | 空 | 抓清单/下载升级包用的 HTTP 代理，如 `http://127.0.0.1:10808` |
| `APP_DIR` | 自动推断 | 在线升级的安装目录（可执行文件所在 `bin/` 的上级） |
| `WEB_ROOT` | 自动推断 | 在线升级的前端静态目录（默认取 `THEMES_DIR` 所在站点的 `index`） |
| `INSECURE_COOKIE` | `false` | 纯 HTTP 部署需设为 `true`，否则浏览器不保存登录 Cookie |
| `BOOTSTRAP_ADMIN_EMAIL/USERNAME/PASSWORD` | 空 / `admin` / 空 | 仅在用户表为空时自动创建管理员 |

---

## 反向代理要点

无论用哪种方式部署，nginx 都需要：

- `/api/` → `127.0.0.1:8081`（保留原始路径）
- `/uploads/`、`/img`、`/sitemap.xml`、`/healthz` → 同上
- 其余路径 `try_files $uri $uri/ /index.html`（SPA 路由）
- 上传上限 `client_max_body_size 100m`
- AI 对话需要较长的 `proxy_read_timeout`（模板里是 300s）

完整模板见 [deploy/nginx.conf.template](deploy/nginx.conf.template)。

---

## 插件

自有插件位于 `extensions/`，每个插件都有独立说明：

| 插件 | 说明 | 文档 |
| --- | --- | --- |
| image-focus | 图片焦点选择：按容器比例裁切时保留重点区域 | [README](extensions/image-focus/README.md) |
| image-viewer | 独立的 `<fanke-img>` 插图标签，支持说明、位置、宽度与圆角 | [README](extensions/image-viewer/README.md) |
| image-processing | 上传时生成等比 WebP 派生图（依赖 `cwebp`） | [README](extensions/image-processing/README.md) |
| s3-storage | S3 兼容多桶对象存储 | [README](extensions/s3-storage/README.md) |
| seo-optimizer | 编辑器 SEO 检查与 AI 建议面板 | [README](extensions/seo-optimizer/README.md) |

安装方式：后台「插件」页上传 zip。部署脚本已把可安装的 zip 放在 `/opt/fanke/dist/extensions/`。

插件开发文档：[doc/plugins.md](doc/plugins.md)；hook 权威参考：[doc/hooks.md](doc/hooks.md)。新增或修改 hook 时必须同步更新文档，并在插件的 `info.json` 中声明。

---

## 主题

文章页与管理页的主题是分开的：文章页默认「材料设计」，管理页默认 Element Plus，可以在后台「设置 → 主题」分别选择或上传。

主题包是一个 zip：

```
my-theme.zip
├── info.json     # 元信息与适用范围
├── site.css      # 文章页样式(可选)
└── admin.css     # 管理页样式(可选)
```

```json
{
  "slug": "my-theme",
  "name": "我的主题",
  "version": "1.0.0",
  "date": "2026-11-15",
  "author": "電腦玩家Stan",
  "description": "示例主题",
  "targets": ["site", "admin"]
}
```

- `targets` 取 `site` / `admin` / 两者;声明了哪个区域就必须提供对应 CSS
- 兼容旧主题:只有 `tokens.css` 时按「文章页 + 管理页」双端主题处理
- 主题 CSS 在运行时注入各自区域,文章页与后台互不覆盖
- 主题也可以在线检查更新与更新（清单 `themes` 段）

---

## 本地开发

后端：

```bash
cd backend
# 准备一个可用的 PostgreSQL，然后：
export DATABASE_URL='postgres://user:pass@127.0.0.1:5432/fanke?sslmode=disable'
export JWT_SECRET="$(openssl rand -hex 32)"
export MEDIA_LOCAL_DIR=./data/media
export THEMES_DIR=./data/themes
export EXTENSIONS_DIR=./data/extensions
export INSECURE_COOKIE=true
go run ./cmd/api
```

前端（默认代理到 `127.0.0.1:8081`，可用 `API_PROXY_TARGET` 覆盖）：

```bash
cd frontend-nuxt
npm install
npm run dev
```

构建检查：

```bash
cd backend && go build ./... && go vet ./...
cd frontend-nuxt && npm run build
```

---

## 安全与备份

- 后端默认只监听 `127.0.0.1`，对外统一由 nginx 终止 TLS
- 登录使用 HttpOnly Cookie；HTTPS 下保持 `INSECURE_COOKIE=false`
- 插件与 `editor.js` 属于受信任代码，只安装来源可信的插件
- 备份三样东西即可完整恢复：
  1. `pg_dump` 导出的数据库
  2. `/opt/fanke/state/fanke.env`
  3. `/opt/fanke/media` 与 `/opt/fanke/themes`（以及 `extensions`）
- `state/fanke.env`、S3 凭证、AI Key 不要提交到仓库

---

## 常见问题

**端口被占用 / 启动失败**
`journalctl -u fanke-api -n 80 --no-pager` 查看日志；改端口用 `--port` 重跑脚本。

**页面能打开但接口 503**
数据库不可达。检查 `systemctl status postgresql` 与 `DATABASE_URL`，必要时用 `RECOVERY_KEY` 走网页找回。

**登录成功但刷新后掉登录态**
纯 HTTP 部署需要 `INSECURE_COOKIE=true`；HTTPS 部署则应为 `false`。

**上传 `.7z` 插件包失败**
安装 `p7zip` / `p7zip-full`；zip 包不受影响。

**图片处理插件不生成 WebP**
确认服务器有 `cwebp`（Debian：`webp`，RHEL：`libwebp-tools`），并在插件详情页启用处理。

**1Panel / 宝塔下访问到默认页**
本脚本写的配置可能被面板默认站点覆盖：在面板中为域名绑定站点或重载 Web 服务；纯净 Linux 下可删除 `/etc/nginx/sites-enabled/default`。

**搜索/摘要没有数据**
首次进入后台后先发布文章；sitemap 与公开列表都直接读数据库。

---

## 许可证

本项目采用 [MIT 许可证](LICENSE)。

Copyright (c) 2026 電腦玩家Stan
