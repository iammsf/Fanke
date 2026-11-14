package repo

import (
	"context"
	"encoding/json"
	"strings"

	"fanke/backend/internal/secret"
)

// EncryptLegacySecrets 把历史明文敏感字段就地加密(幂等,启动时执行一次)。
// 已加密的字段原样保留,因此重复调用无副作用。
func (db *DB) EncryptLegacySecrets(ctx context.Context) error {
	if db == nil || db.Box == nil {
		return nil
	}

	// 1) AI 连接 API Key
	rows, err := db.Query(ctx, `SELECT id, api_key FROM ai_connections WHERE api_key <> ''`)
	if err != nil {
		return err
	}
	type item struct {
		id  int
		key string
	}
	var pending []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.key); err != nil {
			rows.Close()
			return err
		}
		if !secret.Encrypted(it.key) {
			pending = append(pending, it)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, it := range pending {
		if _, err := db.Exec(ctx, `UPDATE ai_connections SET api_key=$2 WHERE id=$1`, it.id, db.seal(it.key)); err != nil {
			return err
		}
	}

	// 2) S3 配置(AccessKey/SecretKey/SessionToken)
	for _, key := range []string{"s3_buckets", "s3_config"} {
		raw, err := db.GetSetting(ctx, key)
		if err != nil {
			continue // 未配置
		}
		if strings.Contains(string(raw), secret.Prefix) {
			continue // 已加密
		}
		if key == "s3_buckets" {
			var s S3Settings
			if json.Unmarshal(raw, &s) != nil {
				continue
			}
			if err := db.PutS3Settings(ctx, s); err != nil {
				return err
			}
			continue
		}
		// 历史单桶配置:整体迁移为多桶格式并加密
		var legacy json.RawMessage
		if json.Unmarshal(raw, &legacy) != nil {
			continue
		}
		if s, err := db.S3Settings(ctx); err == nil && len(s.Buckets) > 0 {
			if err := db.PutS3Settings(ctx, s); err != nil {
				return err
			}
		}
	}
	return nil
}
