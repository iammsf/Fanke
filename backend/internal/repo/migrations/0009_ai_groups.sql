-- AI 连接按自定义名称分组:同组内按 weight 负载均衡,也可指定组内某个模型。
-- 历史连接归入「默认」分组。
ALTER TABLE ai_connections ADD COLUMN IF NOT EXISTS group_name TEXT NOT NULL DEFAULT '默认';
CREATE INDEX IF NOT EXISTS idx_ai_connections_group ON ai_connections(group_name);
