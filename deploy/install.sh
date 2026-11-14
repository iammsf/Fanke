#!/usr/bin/env bash
# fanke 一键部署脚本
# 支持:Debian/Ubuntu、RHEL/CentOS/Rocky/Alma/Fedora,自动识别宝塔面板 / 1Panel。
# 职责:装依赖 → 建库 → 编译后端/前端/插件 → 生成配置 → systemd → nginx 反代。
#
# 用法:
#   sudo bash deploy/install.sh --domain blog.example.com
#   sudo bash deploy/install.sh --domain blog.example.com --port 8081 --dir /opt/fanke
#   sudo bash deploy/install.sh --db-url 'postgres://user:pass@127.0.0.1:5432/fanke?sslmode=disable'
#   sudo bash deploy/install.sh --update          # 升级:重新构建并重启,保留数据与配置
#
# 选项:
#   --domain <域名>       站点域名;不填则用 _ (用服务器 IP 直接访问)
#   --dir <目录>          安装目录,默认 /opt/fanke
#   --port <端口>         后端监听端口,默认 8081(只监听 127.0.0.1,由 nginx 反代)
#   --db-url <URL>        使用已有 PostgreSQL,跳过本地建库
#   --db-name <名称>      本地库名,默认 fanke
#   --db-user <用户>      本地库用户,默认 fanke
#   --db-pass <密码>      本地库密码,默认随机生成
#   --admin-user <用户名> 自动创建首个管理员(默认留空:首次打开网页走安装向导)
#   --admin-email <邮箱>
#   --admin-pass <密码>   至少 10 位
#   --skip-nginx          只部署后端与静态文件,不写 nginx 配置
#   --skip-plugins        不编译/打包 extensions 下的自有插件
#   --update              升级模式(保留配置和数据)
#   -h, --help            显示帮助
set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"

APP_NAME="fanke"
APP_DIR="/opt/fanke"
PORT="8081"
DOMAIN=""
DB_URL=""
DB_NAME="fanke"
DB_USER="fanke"
DB_PASS=""
ADMIN_USER=""
ADMIN_EMAIL=""
ADMIN_PASS=""
SKIP_NGINX=0
SKIP_PLUGINS=0
UPDATE=0
PORT_SET=0
DB_URL_SET=0

log()  { printf '\033[36m[fanke]\033[0m %s\n' "$*"; }
warn() { printf '\033[33m[警告]\033[0m %s\n' "$*"; }
die()  { printf '\033[31m[错误]\033[0m %s\n' "$*" >&2; exit 1; }
trap 'printf "\033[31m[错误]\033[0m 第 %s 行执行失败,请把上方输出发回反馈。\n" "$LINENO" >&2' ERR

usage() { sed -n '2,30p' "${BASH_SOURCE[0]}" | sed -e 's/^# \{0,1\}//'; exit 0; }

while [ $# -gt 0 ]; do
  case "$1" in
    --domain) DOMAIN="${2:-}"; shift 2 ;;
    --dir) APP_DIR="${2:-}"; shift 2 ;;
    --port) PORT="${2:-}"; PORT_SET=1; shift 2 ;;
    --db-url) DB_URL="${2:-}"; DB_URL_SET=1; shift 2 ;;
    --db-name) DB_NAME="${2:-}"; shift 2 ;;
    --db-user) DB_USER="${2:-}"; shift 2 ;;
    --db-pass) DB_PASS="${2:-}"; shift 2 ;;
    --admin-user) ADMIN_USER="${2:-}"; shift 2 ;;
    --admin-email) ADMIN_EMAIL="${2:-}"; shift 2 ;;
    --admin-pass) ADMIN_PASS="${2:-}"; shift 2 ;;
    --skip-nginx) SKIP_NGINX=1; shift ;;
    --skip-plugins) SKIP_PLUGINS=1; shift ;;
    --update) UPDATE=1; shift ;;
    -h|--help) usage ;;
    *) die "未知参数: $1(用 --help 查看用法)" ;;
  esac
done

# ---------- 基础校验 ----------
[ -f "$REPO_ROOT/backend/go.mod" ] || die "找不到 backend/go.mod,请在仓库根目录内运行 deploy/install.sh"
case "$PORT" in
  ''|*[!0-9]*) die "--port 必须是数字" ;;
esac
[ "$PORT" -ge 1 ] && [ "$PORT" -le 65535 ] || die "--port 超出范围"
case "$DB_NAME$DB_USER" in
  *[!A-Za-z0-9_]*) die "--db-name / --db-user 只允许字母、数字、下划线" ;;
esac
if [ -n "$DB_PASS" ]; then
  case "$DB_PASS" in
    *[!A-Za-z0-9_.-]*) die "--db-pass 只支持字母、数字、下划线、点、短横线;留空可自动生成随机密码" ;;
  esac
fi
if [ -n "$DOMAIN" ]; then
  DOMAIN="${DOMAIN#http://}"; DOMAIN="${DOMAIN#https://}"; DOMAIN="${DOMAIN%%/*}"
  case "$DOMAIN" in *[!A-Za-z0-9.-]*) die "--domain 格式不正确";; esac
fi

if [ "$(id -u)" -ne 0 ]; then
  command -v sudo >/dev/null 2>&1 || die "请用 root 运行:bash deploy/install.sh"
  log "需要 root 权限,正在通过 sudo 重新执行..."
  exec sudo -E bash "$0" "$@"
fi

CONFIG_FILE="$APP_DIR/state/fanke.env"

# 复用已有数据库连接(升级/重跑时不重建)
if [ -z "$DB_URL" ] && [ -f "$CONFIG_FILE" ]; then
  DB_URL="$(sed -n 's/^DATABASE_URL=//p' "$CONFIG_FILE" | head -n1)"
  if [ -n "$DB_URL" ]; then log "沿用已有 DATABASE_URL"; fi
fi

# ---------- 系统识别 ----------
if [ -r /etc/os-release ]; then
  # shellcheck disable=SC1091
  . /etc/os-release
fi
OS_NAME="${PRETTY_NAME:-${ID:-unknown}}"

if command -v apt-get >/dev/null 2>&1; then PKG=apt
elif command -v dnf >/dev/null 2>&1; then PKG=dnf
elif command -v yum >/dev/null 2>&1; then PKG=yum
else die "未识别的包管理器(仅支持 apt/dnf/yum)"; fi

PANEL="none"
if [ -d /www/server/panel ]; then PANEL="baota"
elif [ -d /opt/1panel ]; then PANEL="1panel"
fi

case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64"; NODE_ARCH="x64" ;;
  aarch64|arm64) ARCH="arm64"; NODE_ARCH="arm64" ;;
  *) die "不支持的 CPU 架构: $(uname -m)" ;;
esac

log "系统: $OS_NAME | 包管理器: $PKG | 架构: $ARCH | 面板: $PANEL"
if [ "$UPDATE" -eq 1 ]; then log "升级模式:保留 $APP_DIR 下的数据与配置"; fi

# ---------- 依赖安装 ----------
pkg_install() {
  [ $# -gt 0 ] || return 0
  case "$PKG" in
    apt) DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "$@" ;;
    dnf) dnf install -y "$@" ;;
    yum) yum install -y "$@" ;;
  esac
}

log "更新软件源并安装基础依赖..."
case "$PKG" in
  apt) DEBIAN_FRONTEND=noninteractive apt-get update -y ;;
  yum) yum install -y epel-release >/dev/null 2>&1 || true ;;
esac

# 核心依赖:缺失即失败
CORE_PKGS=(curl ca-certificates tar xz openssl zip unzip)
case "$PKG" in
  apt)
    if [ "$PANEL" = "none" ]; then CORE_PKGS+=(nginx); fi
    if [ -z "$DB_URL" ]; then CORE_PKGS+=(postgresql postgresql-contrib); fi
    ;;
  dnf|yum)
    if [ "$PANEL" = "none" ]; then CORE_PKGS+=(nginx); fi
    if [ -z "$DB_URL" ]; then CORE_PKGS+=(postgresql-server); fi
    ;;
esac
pkg_install "${CORE_PKGS[@]}"

# 可选依赖:装不上只告警,不阻断主流程
case "$PKG" in
  apt)
    pkg_install p7zip-full || warn "p7zip 安装失败:后台将无法上传 .7z 插件包(zip 不受影响)"
    pkg_install webp || warn "cwebp 安装失败:图片处理插件无法生成 WebP"
    ;;
  dnf|yum)
    pkg_install p7zip p7zip-plugins || warn "p7zip 安装失败:后台将无法上传 .7z 插件包(zip 不受影响)"
    pkg_install libwebp-tools || warn "cwebp 安装失败:图片处理插件无法生成 WebP"
    ;;
esac

# ---------- Go ----------
version_ge() { [ "$1" = "$2" ] || [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -n1)" = "$2" ]; }

ensure_go() {
  local want have ver
  want="$(awk '/^go /{print $2; exit}' "$REPO_ROOT/backend/go.mod")"
  [ -n "$want" ] || die "无法从 backend/go.mod 读取 Go 版本"
  have=""
  if command -v go >/dev/null 2>&1; then
    have="$(go env GOVERSION 2>/dev/null | sed 's/^go//')"
  fi
  if [ -n "$have" ] && version_ge "$have" "$want"; then
    log "Go $have 已满足要求(>= $want)"
    return 0
  fi
  log "安装 Go >= $want ..."
  ver="$(curl -fsSL 'https://go.dev/dl/?mode=json' 2>/dev/null | tr -d ' \n' \
    | grep -o "\"version\":\"go${want}[^\"]*\"" | head -n1 | cut -d'"' -f4 || true)"
  [ -n "$ver" ] || ver="go${want}.0"
  curl -fL --retry 3 -o /tmp/fanke-go.tar.gz "https://go.dev/dl/${ver}.linux-${ARCH}.tar.gz"
  rm -rf /usr/local/go
  tar -C /usr/local -xzf /tmp/fanke-go.tar.gz
  rm -f /tmp/fanke-go.tar.gz
  ln -sf /usr/local/go/bin/go /usr/local/bin/go
  ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
  hash -r
  log "Go 安装完成: $(go env GOVERSION)"
}

# ---------- Node.js ----------
ensure_node() {
  local major=0 ver
  if command -v node >/dev/null 2>&1; then
    major="$(node -p 'process.versions.node.split(".")[0]' 2>/dev/null || echo 0)"
  fi
  if [ "$major" -ge 18 ] 2>/dev/null; then
    log "Node.js $(node -v) 已满足要求(>= 18)"
    return 0
  fi
  log "安装 Node.js 22 ..."
  ver="$(curl -fsSL https://nodejs.org/dist/index.json | tr -d ' \n' \
    | grep -o '"version":"v22[^"]*"' | head -n1 | cut -d'"' -f4 || true)"
  [ -n "$ver" ] || die "无法获取 Node.js 版本信息"
  curl -fL --retry 3 -o /tmp/fanke-node.tar.xz "https://nodejs.org/dist/${ver}/node-${ver}-linux-${NODE_ARCH}.tar.xz"
  tar -C /usr/local --strip-components=1 -xJf /tmp/fanke-node.tar.xz
  rm -f /tmp/fanke-node.tar.xz
  hash -r
  log "Node.js 安装完成: $(node -v)"
}

ensure_go
ensure_node

# ---------- PostgreSQL ----------
ensure_postgres() {
  log "启动 PostgreSQL ..."
  case "$PKG" in
    apt) systemctl enable --now postgresql >/dev/null 2>&1 || systemctl restart postgresql ;;
    *)
      if [ ! -f /var/lib/pgsql/data/PG_VERSION ]; then
        postgresql-setup --initdb >/dev/null 2>&1 || postgresql-setup initdb >/dev/null 2>&1 || true
      fi
      systemctl enable --now postgresql >/dev/null 2>&1 || systemctl restart postgresql
      ;;
  esac
  local i
  for i in $(seq 1 30); do
    if su - postgres -c "psql -tAc 'SELECT 1'" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  die "PostgreSQL 未能启动,请检查 systemctl status postgresql"
}

provision_db() {
  ensure_postgres
  [ -n "$DB_PASS" ] || DB_PASS="$(openssl rand -hex 16)"
  local exists
  exists="$(su - postgres -c "psql -tAc \"SELECT 1 FROM pg_roles WHERE rolname='$DB_USER'\"" | tr -d '[:space:]')"
  if [ "$exists" != "1" ]; then
    su - postgres -c "psql -v ON_ERROR_STOP=1 -c \"CREATE ROLE $DB_USER LOGIN PASSWORD '$DB_PASS'\""
  else
    su - postgres -c "psql -v ON_ERROR_STOP=1 -c \"ALTER ROLE $DB_USER WITH LOGIN PASSWORD '$DB_PASS'\""
  fi
  exists="$(su - postgres -c "psql -tAc \"SELECT 1 FROM pg_database WHERE datname='$DB_NAME'\"" | tr -d '[:space:]')"
  if [ "$exists" != "1" ]; then
    su - postgres -c "psql -v ON_ERROR_STOP=1 -c \"CREATE DATABASE $DB_NAME OWNER $DB_USER\""
  fi
  su - postgres -c "psql -d $DB_NAME -v ON_ERROR_STOP=1 -c \"GRANT ALL ON SCHEMA public TO $DB_USER\"" >/dev/null
  DB_URL="postgres://$DB_USER:$DB_PASS@127.0.0.1:5432/$DB_NAME?sslmode=disable"
  log "本地数据库就绪: $DB_NAME(用户 $DB_USER)"
}

if [ -z "$DB_URL" ]; then
  provision_db
else
  log "使用已有数据库连接(跳过本地建库)"
fi

# ---------- 目录与系统用户 ----------
if ! id -u "$APP_NAME" >/dev/null 2>&1; then
  useradd --system --home "$APP_DIR" --shell /usr/sbin/nologin "$APP_NAME" 2>/dev/null \
    || useradd -r -d "$APP_DIR" -s /sbin/nologin "$APP_NAME"
fi
install -d -m 0755 -o "$APP_NAME" -g "$APP_NAME" \
  "$APP_DIR" "$APP_DIR/bin" "$APP_DIR/themes" "$APP_DIR/extensions" "$APP_DIR/media" "$APP_DIR/dist" "$APP_DIR/dist/extensions"
install -d -m 0700 -o "$APP_NAME" -g "$APP_NAME" "$APP_DIR/state"
install -d -m 0755 -o "$APP_NAME" -g "$APP_NAME" /var/cache/fanke-thumbs

# ---------- 编译 ----------
build_backend() {
  log "编译后端(linux/$ARCH)..."
  ( cd "$REPO_ROOT/backend" \
    && CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -trimpath -buildvcs=false \
       -ldflags "-s -w" -o "$APP_DIR/bin/api.new" ./cmd/api )
  chmod 0755 "$APP_DIR/bin/api.new"
  mv -f "$APP_DIR/bin/api.new" "$APP_DIR/bin/api"
}

build_frontend() {
  log "安装前端依赖..."
  if [ -f "$REPO_ROOT/frontend-nuxt/package-lock.json" ]; then
    ( cd "$REPO_ROOT/frontend-nuxt" && npm ci --no-audit --no-fund )
  else
    ( cd "$REPO_ROOT/frontend-nuxt" && npm install --no-audit --no-fund )
  fi
  log "构建前端静态文件..."
  ( cd "$REPO_ROOT/frontend-nuxt" \
    && NUXT_TELEMETRY_DISABLED=1 NUXT_PUBLIC_API_BASE=/api/v1 \
       NODE_OPTIONS="${NODE_OPTIONS:-} --max-old-space-size=2048" \
       npm run build )
  [ -f "$REPO_ROOT/frontend-nuxt/.output/public/index.html" ] || die "前端构建失败:缺少 .output/public/index.html"
}

build_plugins() {
  local d slug
  log "编译并打包自有插件..."
  for d in "$REPO_ROOT"/extensions/*/; do
    [ -d "$d" ] || continue
    [ -f "$d/info.json" ] || continue
    slug="$(basename "$d")"
    if [ -f "$d/go.mod" ] && [ -d "$d/server" ]; then
      log "  编译插件服务端: $slug"
      ( cd "$d" \
        && CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -trimpath -buildvcs=false \
           -ldflags "-s -w" -o "server/bin/linux-$ARCH/$slug-server" ./server )
    fi
    rm -f "$APP_DIR/dist/extensions/$slug.zip"
    ( cd "$d" && zip -qr "$APP_DIR/dist/extensions/$slug.zip" . )
    log "  已打包 $APP_DIR/dist/extensions/$slug.zip"
  done
}

build_backend
build_frontend
[ "$SKIP_PLUGINS" -eq 1 ] || build_plugins

# ---------- 静态目录 ----------
SITE_NAME="${DOMAIN:-$APP_NAME}"
case "$PANEL" in
  baota)
    WEB_ROOT="/www/wwwroot/$SITE_NAME"
    ROOT_IN_CONF="$WEB_ROOT"
    ;;
  1panel)
    WEB_ROOT="/opt/1panel/www/sites/$SITE_NAME/index"
    ROOT_IN_CONF="/www/sites/$SITE_NAME/index"
    ;;
  *)
    WEB_ROOT="$APP_DIR/web"
    ROOT_IN_CONF="$WEB_ROOT"
    ;;
esac
install -d -m 0755 "$WEB_ROOT"
find "$WEB_ROOT" -mindepth 1 -maxdepth 1 -exec rm -rf {} +
cp -a "$REPO_ROOT/frontend-nuxt/.output/public/." "$WEB_ROOT/"
chown -R "$APP_NAME:$APP_NAME" "$APP_DIR"
log "前端已部署到 $WEB_ROOT"

# ---------- 运行时配置 ----------
write_env() {
  local jwt rec
  if [ -f "$CONFIG_FILE" ]; then
    if [ "$DB_URL_SET" -eq 1 ]; then
      sed -i "s|^DATABASE_URL=.*|DATABASE_URL=$DB_URL|" "$CONFIG_FILE"
    fi
    if [ "$PORT_SET" -eq 1 ]; then
      sed -i "s|^ADDR=.*|ADDR=127.0.0.1:$PORT|" "$CONFIG_FILE"
    fi
    log "保留已有配置 $CONFIG_FILE"
    return 0
  fi
  jwt="$(openssl rand -hex 32)"
  rec="$(openssl rand -hex 16)"
  umask 077
  cat > "$CONFIG_FILE" <<EOF
# fanke 运行时配置(由 deploy/install.sh 生成,0600;修改后重启 fanke-api)
DATABASE_URL=$DB_URL
JWT_SECRET=$jwt
RECOVERY_KEY=$rec
ADDR=127.0.0.1:$PORT
MEDIA_LOCAL_DIR=$APP_DIR/media
MEDIA_BASE_URL=/uploads
THEMES_DIR=$APP_DIR/themes
EXTENSIONS_DIR=$APP_DIR/extensions
THUMB_DIR=/var/cache/fanke-thumbs
THUMB_ALLOW_HOSTS=
# 初始为 HTTP 部署,登录 Cookie 不能带 Secure;启用 HTTPS 后请改为 false 并重启服务
INSECURE_COOKIE=true
EOF
  if [ -n "$ADMIN_USER" ] && [ -n "$ADMIN_EMAIL" ] && [ -n "$ADMIN_PASS" ]; then
    cat >> "$CONFIG_FILE" <<EOF
BOOTSTRAP_ADMIN_USERNAME=$ADMIN_USER
BOOTSTRAP_ADMIN_EMAIL=$ADMIN_EMAIL
BOOTSTRAP_ADMIN_PASSWORD=$ADMIN_PASS
EOF
  fi
  chown "$APP_NAME:$APP_NAME" "$CONFIG_FILE"
  chmod 0600 "$CONFIG_FILE"
  log "已生成配置 $CONFIG_FILE"
}
write_env

# ---------- systemd ----------
log "安装 systemd 服务..."
sed -e "s|__DIR__|$APP_DIR|g" "$SCRIPT_DIR/fanke-api.service" > /etc/systemd/system/fanke-api.service
systemctl daemon-reload
systemctl enable "$APP_NAME-api" >/dev/null 2>&1 || true
systemctl restart "$APP_NAME-api"

# ---------- nginx ----------
configure_nginx() {
  local conf_dir conf_path
  case "$PANEL" in
    baota)
      NGINX_BIN="/www/server/nginx/sbin/nginx"
      conf_path="/www/server/panel/vhost/nginx/fanke-$SITE_NAME.conf"
      ;;
    1panel)
      NGINX_BIN=""
      conf_dir=""
      for d in /opt/1panel/apps/openresty/openresty/conf/conf.d \
               /opt/1panel/apps/openresty/openresty/conf.d \
               /opt/1panel/www/conf.d; do
        if [ -d "$d" ]; then conf_dir="$d"; break; fi
      done
      if [ -n "$conf_dir" ]; then
        conf_path="$conf_dir/fanke-$SITE_NAME.conf"
      else
        conf_path="/etc/nginx/conf.d/fanke-$SITE_NAME.conf"
        warn "未找到 1Panel OpenResty 的 conf.d 目录,改为写入 $conf_path,可能不会生效"
      fi
      ;;
    *)
      NGINX_BIN="$(command -v nginx || true)"
      conf_path="/etc/nginx/conf.d/fanke.conf"
      if [ -z "$DOMAIN" ] && [ -f /etc/nginx/sites-enabled/default ]; then
        warn "未指定 --domain,且系统存在默认站点 /etc/nginx/sites-enabled/default;若 IP 访问看到默认页,请删除该软链后重载 nginx"
      fi
      # SELinux 环境下放行 nginx 读站点目录与反代
      if command -v getenforce >/dev/null 2>&1 && [ "$(getenforce 2>/dev/null || echo Disabled)" != "Disabled" ]; then
        chcon -R -t httpd_sys_content_t "$WEB_ROOT" 2>/dev/null || true
        setsebool -P httpd_can_network_connect 1 2>/dev/null || true
      fi
      ;;
  esac

  sed -e "s|__SERVER_NAME__|${DOMAIN:-_}|g" \
      -e "s|__ROOT__|$ROOT_IN_CONF|g" \
      -e "s|__PORT__|$PORT|g" \
      "$SCRIPT_DIR/nginx.conf.template" > "$conf_path"
  log "已写入 nginx 配置 $conf_path"

  case "$PANEL" in
    1panel)
      local c
      c="$(docker ps --format '{{.Names}}' 2>/dev/null | grep -i openresty | head -n1 || true)"
      if [ -n "$c" ]; then
        docker exec "$c" openresty -t && docker exec "$c" openresty -s reload \
          || warn "OpenResty reload 失败,请在 1Panel 中重载 OpenResty"
      else
        warn "未找到 OpenResty 容器,请在 1Panel 面板中重载 OpenResty 使配置生效"
      fi
      ;;
    baota)
      if [ -x "$NGINX_BIN" ]; then
        "$NGINX_BIN" -t && "$NGINX_BIN" -s reload
      else
        warn "未找到宝塔 nginx 可执行文件 $NGINX_BIN"
      fi
      ;;
    *)
      if [ -n "$NGINX_BIN" ]; then
        "$NGINX_BIN" -t && systemctl reload nginx
      else
        warn "未安装 nginx,已跳过反代配置"
      fi
      ;;
  esac
}
if [ "$SKIP_NGINX" -eq 1 ]; then
  warn "已按参数跳过 nginx 配置"
else
  configure_nginx
fi

# ---------- 健康检查 ----------
log "等待后端启动..."
ok=0
for _ in $(seq 1 30); do
  code="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/healthz" || true)"
  if [ "$code" = "200" ]; then ok=1; break; fi
  sleep 1
done
if [ "$ok" -eq 1 ]; then
  log "后端健康检查通过"
else
  warn "后端健康检查未通过,请执行: journalctl -u fanke-api -n 80 --no-pager"
fi

# ---------- 摘要 ----------
cat <<EOF

============================================================
 fanke 部署完成
------------------------------------------------------------
 访问地址   : http://${DOMAIN:-<服务器IP>}
 首次使用   : 打开上面的地址,按向导创建管理员账号
 安装目录   : $APP_DIR
 配置文件   : $CONFIG_FILE  (含数据库密码与找回密钥,权限 0600)
 插件安装包 : $APP_DIR/dist/extensions/*.zip(后台「插件」页上传)
 服务管理   : systemctl status|restart|stop fanke-api
 查看日志   : journalctl -u fanke-api -f
------------------------------------------------------------
 提示:
 - 数据库/密钥都在 $CONFIG_FILE,请一并纳入备份,不要泄露。
 - 当前配置为 HTTP(INSECURE_COOKIE=true);启用 HTTPS 后请把它改成 false 并重启。
 - 启用 HTTPS 后,建议把站点配置里的 80 端口跳转到 443。
 - 纯 Linux:可执行 certbot --nginx -d ${DOMAIN:-your-domain} 申请证书。
 - 宝塔/1Panel:在面板里为站点 ${SITE_NAME} 绑定域名并申请证书即可。
============================================================
EOF
