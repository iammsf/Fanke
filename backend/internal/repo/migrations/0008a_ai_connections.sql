CREATE TABLE ai_connections (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    provider TEXT NOT NULL, -- 'openai' or 'claude'
    api_url TEXT NOT NULL,
    api_key TEXT NOT NULL,
    model TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    weight INTEGER NOT NULL DEFAULT 1, -- 负载均衡权重
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_ai_connections_enabled ON ai_connections(enabled) WHERE enabled;
