-- 热路径缺失索引。以下四处都在每次公开访问时命中，此前只能全表扫：
--   media(url)   —— MediaDimsByURLs(每篇文章渲染注入 width/height/srcset)、MediaByURL(每个 /img 缩略图请求)
--   media(key)   —— MediaKeyExists(S3 同步入库查重)。UNIQUE(storage,key) 最左列是 storage，key 单查用不上
--   post_terms(term_id) —— 标签/分类归档 JOIN(posts.go 按 t.slug 过滤)、terms 列表的文章计数。主键是 (post_id,term_id)，反向查用不上
--   comments(status, created_at DESC) —— 管理端评论列表 count(*) + ORDER BY created_at DESC，
--                                        此前只有 comments_post_idx(post_id,status)，跨文章列表无法命中
CREATE INDEX IF NOT EXISTS media_url_idx ON media (url);
CREATE INDEX IF NOT EXISTS media_key_idx ON media (key);
CREATE INDEX IF NOT EXISTS post_terms_term_idx ON post_terms (term_id);
CREATE INDEX IF NOT EXISTS comments_status_created_idx ON comments (status, created_at DESC);
