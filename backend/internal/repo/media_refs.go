package repo

import "context"

// HasS3MediaReferences reports whether original media or derived variants still use S3.
func (db *DB) HasS3MediaReferences(ctx context.Context) (bool, error) {
	var found bool
	err := db.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM media WHERE storage='s3'
		UNION ALL
		SELECT 1 FROM media_variants WHERE storage='s3'
	)`).Scan(&found)
	return found, err
}
