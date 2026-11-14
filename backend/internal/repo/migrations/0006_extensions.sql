CREATE TABLE extensions_data (
  id           BIGSERIAL PRIMARY KEY,
  slug         TEXT NOT NULL UNIQUE,           -- 由 name 派生的安全标识,用于 URL/目录
  name         TEXT NOT NULL,
  author       TEXT NOT NULL DEFAULT '',
  version      TEXT NOT NULL DEFAULT '',
  date         TEXT NOT NULL DEFAULT '',       -- 插件作者自填的发布日期
  description  TEXT NOT NULL DEFAULT '',
  url          TEXT NOT NULL DEFAULT '',       -- 作者主页
  update_url   TEXT NOT NULL DEFAULT '',       -- 检查更新地址
  favico       TEXT NOT NULL DEFAULT '',       -- 包内图标相对路径(可为空)
  hooks        JSONB NOT NULL DEFAULT '[]',    -- info.json 里 request 声明的 hook 列表
  enabled      BOOLEAN NOT NULL DEFAULT FALSE, -- 安装后默认禁用,详情页初始化完成才允许启用
  initialized  BOOLEAN NOT NULL DEFAULT FALSE, -- init.js 是否已执行完成
  installed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
