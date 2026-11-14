-- 改密即全局下线:记录密码最后修改时间,签发时间早于它的 access token 一律失效
ALTER TABLE users ADD COLUMN IF NOT EXISTS pwd_changed_at TIMESTAMPTZ;
