CREATE TABLE plugins (
  id         BIGSERIAL PRIMARY KEY,
  name       TEXT NOT NULL UNIQUE,
  css        TEXT NOT NULL DEFAULT '',
  js         TEXT NOT NULL DEFAULT '',
  enabled    BOOLEAN NOT NULL DEFAULT TRUE,
  sort       INT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
