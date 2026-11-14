-- Preserve all legacy configuration fields, including credentials and public URLs.
INSERT INTO settings(key, value)
SELECT 's3_config', value FROM settings WHERE key = 'r2_config'
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value || settings.value;
DELETE FROM settings WHERE key = 'r2_config';
UPDATE settings SET value = value || jsonb_build_object('force_path_style', COALESCE(value->'force_path_style', value->'path_style', 'true'::jsonb)) WHERE key = 's3_config';

ALTER TABLE media DROP CONSTRAINT IF EXISTS media_storage_check;
UPDATE media SET storage = 's3' WHERE storage = 'r2';
UPDATE media_variants SET storage = 's3' WHERE storage = 'r2';
ALTER TABLE media ADD CONSTRAINT media_storage_check CHECK (storage IN ('s3', 'local'));
ALTER TABLE media_variants ADD CONSTRAINT media_variants_storage_check CHECK (storage IN ('s3', 'local'));
