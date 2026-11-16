-- 多桶上传把 storage 写成 's3:<桶ID>'（handlers_media.go / handlers_media_sync.go / media_plugins.go），
-- 0008b 的 CHECK 只允许 's3'/'local'，会让多桶上传在写库时被拒并回删已上传对象。
ALTER TABLE media DROP CONSTRAINT IF EXISTS media_storage_check;
ALTER TABLE media_variants DROP CONSTRAINT IF EXISTS media_variants_storage_check;
ALTER TABLE media ADD CONSTRAINT media_storage_check CHECK (storage = 'local' OR storage = 's3' OR storage LIKE 's3:%');
ALTER TABLE media_variants ADD CONSTRAINT media_variants_storage_check CHECK (storage = 'local' OR storage = 's3' OR storage LIKE 's3:%');
