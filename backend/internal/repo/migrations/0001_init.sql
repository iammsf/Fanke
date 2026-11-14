CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE users (
  id             BIGSERIAL PRIMARY KEY,
  email          TEXT NOT NULL UNIQUE,
  username       TEXT NOT NULL UNIQUE,
  password_hash  TEXT NOT NULL,
  role           TEXT NOT NULL CHECK (role IN ('admin','author')),
  display_name   TEXT NOT NULL,
  avatar_url     TEXT NOT NULL DEFAULT '',
  bio            TEXT NOT NULL DEFAULT '',
  failed_attempts INT NOT NULL DEFAULT 0,
  locked_until   TIMESTAMPTZ,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE refresh_tokens (
  jti        TEXT PRIMARY KEY,
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TIMESTAMPTZ NOT NULL,
  revoked    BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE media (
  id         BIGSERIAL PRIMARY KEY,
  filename   TEXT NOT NULL,
  storage    TEXT NOT NULL CHECK (storage IN ('r2','local')),
  key        TEXT NOT NULL,
  url        TEXT NOT NULL,
  mime       TEXT NOT NULL,
  size       BIGINT NOT NULL,
  width      INT NOT NULL DEFAULT 0,
  height     INT NOT NULL DEFAULT 0,
  sha256     TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (storage, key)
);
CREATE INDEX media_sha_idx ON media (sha256);

CREATE TABLE posts (
  id             BIGSERIAL PRIMARY KEY,
  slug           TEXT NOT NULL UNIQUE,
  title          TEXT NOT NULL,
  summary        TEXT NOT NULL DEFAULT '',
  content_md     TEXT NOT NULL DEFAULT '',
  content_html   TEXT NOT NULL DEFAULT '',
  status         TEXT NOT NULL CHECK (status IN ('draft','published')) DEFAULT 'draft',
  author_id      BIGINT NOT NULL REFERENCES users(id),
  cover_media_id BIGINT REFERENCES media(id),
  allow_comments BOOLEAN NOT NULL DEFAULT TRUE,
  views          BIGINT NOT NULL DEFAULT 0,
  published_at   TIMESTAMPTZ,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX posts_pub_idx ON posts (published_at DESC) WHERE status = 'published';
CREATE INDEX posts_title_trgm_idx ON posts USING gin (title gin_trgm_ops);
CREATE INDEX posts_content_trgm_idx ON posts USING gin (content_md gin_trgm_ops);

CREATE TABLE terms (
  id   BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  slug TEXT NOT NULL,
  type TEXT NOT NULL CHECK (type IN ('category','tag')),
  UNIQUE (slug, type)
);

CREATE TABLE post_terms (
  post_id BIGINT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  term_id BIGINT NOT NULL REFERENCES terms(id) ON DELETE CASCADE,
  PRIMARY KEY (post_id, term_id)
);

CREATE TABLE comments (
  id           BIGSERIAL PRIMARY KEY,
  post_id      BIGINT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  parent_id    BIGINT REFERENCES comments(id) ON DELETE CASCADE,
  author_name  TEXT NOT NULL,
  author_email TEXT NOT NULL DEFAULT '',
  user_id      BIGINT REFERENCES users(id),
  content      TEXT NOT NULL,
  status       TEXT NOT NULL CHECK (status IN ('pending','approved','spam')) DEFAULT 'pending',
  ip_hash      TEXT NOT NULL DEFAULT '',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX comments_post_idx ON comments (post_id, status);

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value JSONB NOT NULL
);
