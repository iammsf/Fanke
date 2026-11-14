CREATE TABLE media_variants (
    media_id BIGINT NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    width INTEGER NOT NULL CHECK (width > 0),
    height INTEGER NOT NULL CHECK (height > 0),
    storage TEXT NOT NULL,
    key TEXT NOT NULL,
    url TEXT NOT NULL,
    size BIGINT NOT NULL,
    PRIMARY KEY (media_id, width),
    UNIQUE (storage, url)
);
INSERT INTO settings(key, value) VALUES ('media_image_upload', '{"enabled":false}'::jsonb)
ON CONFLICT (key) DO NOTHING;
