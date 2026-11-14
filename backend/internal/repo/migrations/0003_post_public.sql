-- 文章公开性:is_public=false 的文章仅后台可见,不进入任何前端(公开)接口
ALTER TABLE posts ADD COLUMN IF NOT EXISTS is_public BOOLEAN NOT NULL DEFAULT TRUE;

-- 公开列表查询的部分索引(替换旧的 posts_pub_idx)
DROP INDEX IF EXISTS posts_pub_idx;
CREATE INDEX IF NOT EXISTS posts_pub_idx ON posts (published_at DESC) WHERE status = 'published' AND is_public;
