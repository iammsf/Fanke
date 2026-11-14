-- 文章类型(standard/moment)与置顶(sticky>0 为置顶,数值越小越靠前)
ALTER TABLE posts ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT 'standard';
ALTER TABLE posts ADD COLUMN IF NOT EXISTS sticky INT NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS posts_sticky_idx ON posts(sticky) WHERE sticky > 0;
