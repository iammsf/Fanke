package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// AIConnection 一条 AI 连接即"某个分组下的一个模型"。
// 同组连接共享凭据,调用时按 weight 做加权随机;也可显式指定组内模型。
type AIConnection struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	GroupName string    `json:"group_name"`
	Provider  string    `json:"provider"` // openai | claude
	APIURL    string    `json:"api_url"`
	APIKey    string    `json:"api_key"`
	Model     string    `json:"model"`
	Enabled   bool      `json:"enabled"`
	Weight    int       `json:"weight"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateAIConnection struct {
	Name      string `json:"name"`
	GroupName string `json:"group_name"`
	Provider  string `json:"provider"`
	APIURL    string `json:"api_url"`
	APIKey    string `json:"api_key"`
	Model     string `json:"model"`
	Enabled   bool   `json:"enabled"`
	Weight    int    `json:"weight"`
}

type UpdateAIConnection struct {
	Name      *string `json:"name,omitempty"`
	GroupName *string `json:"group_name,omitempty"`
	Provider  *string `json:"provider,omitempty"`
	APIURL    *string `json:"api_url,omitempty"`
	APIKey    *string `json:"api_key,omitempty"`
	Model     *string `json:"model,omitempty"`
	Enabled   *bool   `json:"enabled,omitempty"`
	Weight    *int    `json:"weight,omitempty"`
}

const aiConnCols = `id, name, group_name, provider, api_url, api_key, model, enabled, weight, created_at, updated_at`

func (db *DB) scanAIConnection(row pgx.Row) (*AIConnection, error) {
	var c AIConnection
	err := row.Scan(&c.ID, &c.Name, &c.GroupName, &c.Provider, &c.APIURL, &c.APIKey,
		&c.Model, &c.Enabled, &c.Weight, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.APIKey = db.unseal(c.APIKey) // 兼容历史明文;新写入均为密文
	return &c, nil
}

func (db *DB) ListAIConnections(ctx context.Context, enabledOnly bool) ([]AIConnection, error) {
	q := `SELECT ` + aiConnCols + ` FROM ai_connections`
	if enabledOnly {
		q += ` WHERE enabled`
	}
	q += ` ORDER BY group_name, id`
	rows, err := db.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AIConnection
	for rows.Next() {
		c, err := db.scanAIConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (db *DB) GetAIConnection(ctx context.Context, id int) (AIConnection, error) {
	c, err := db.scanAIConnection(db.QueryRow(ctx, `SELECT `+aiConnCols+` FROM ai_connections WHERE id=$1`, id))
	if err != nil {
		return AIConnection{}, err
	}
	return *c, nil
}

const insertAIConnection = `INSERT INTO ai_connections(name, group_name, provider, api_url, api_key, model, enabled, weight)
	VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING ` + aiConnCols

func (db *DB) CreateAIConnection(ctx context.Context, c CreateAIConnection) (AIConnection, error) {
	out, err := db.scanAIConnection(db.QueryRow(ctx, insertAIConnection,
		c.Name, c.GroupName, c.Provider, c.APIURL, db.seal(c.APIKey), c.Model, c.Enabled, c.Weight))
	if err != nil {
		return AIConnection{}, err
	}
	return *out, nil
}

// CreateAIConnections 一次添加同组多个模型(单事务,避免部分成功)。
func (db *DB) CreateAIConnections(ctx context.Context, items []CreateAIConnection) ([]AIConnection, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) // 已提交后调用无副作用
	out := make([]AIConnection, 0, len(items))
	for _, c := range items {
		conn, err := db.scanAIConnection(tx.QueryRow(ctx, insertAIConnection,
			c.Name, c.GroupName, c.Provider, c.APIURL, db.seal(c.APIKey), c.Model, c.Enabled, c.Weight))
		if err != nil {
			return nil, err
		}
		out = append(out, *conn)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (db *DB) UpdateAIConnection(ctx context.Context, id int, u UpdateAIConnection) (AIConnection, error) {
	sets := []string{"updated_at=now()"}
	args := []any{id}
	idx := 2
	add := func(col string, v any) {
		sets = append(sets, fmt.Sprintf("%s=$%d", col, idx))
		args = append(args, v)
		idx++
	}
	if u.Name != nil {
		add("name", *u.Name)
	}
	if u.GroupName != nil {
		add("group_name", *u.GroupName)
	}
	if u.Provider != nil {
		add("provider", *u.Provider)
	}
	if u.APIURL != nil {
		add("api_url", *u.APIURL)
	}
	if u.APIKey != nil {
		add("api_key", db.seal(*u.APIKey))
	}
	if u.Model != nil {
		add("model", *u.Model)
	}
	if u.Enabled != nil {
		add("enabled", *u.Enabled)
	}
	if u.Weight != nil {
		add("weight", *u.Weight)
	}
	out, err := db.scanAIConnection(db.QueryRow(ctx, `UPDATE ai_connections SET `+strings.Join(sets, ",")+`
		WHERE id=$1 RETURNING `+aiConnCols, args...))
	if err != nil {
		return AIConnection{}, err
	}
	return *out, nil
}

// RenameAIGroup 重命名整个分组(组内所有连接)。
func (db *DB) RenameAIGroup(ctx context.Context, from, to string) (int64, error) {
	tag, err := db.Exec(ctx, `UPDATE ai_connections SET group_name=$2, updated_at=now() WHERE group_name=$1`, from, to)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (db *DB) DeleteAIConnection(ctx context.Context, id int) error {
	_, err := db.Exec(ctx, `DELETE FROM ai_connections WHERE id=$1`, id)
	return err
}

// DeleteAIGroup 删除整个分组:target 为空则连带删除组内模型,否则把模型迁移到 target 分组。
func (db *DB) DeleteAIGroup(ctx context.Context, name, target string) (int64, error) {
	if target == "" {
		tag, err := db.Exec(ctx, `DELETE FROM ai_connections WHERE group_name=$1`, name)
		if err != nil {
			return 0, err
		}
		return tag.RowsAffected(), nil
	}
	tag, err := db.Exec(ctx, `UPDATE ai_connections SET group_name=$2, updated_at=now() WHERE group_name=$1`, name, target)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// GroupCredentials 取分组内最近更新的一条连接的凭据,
// 供"往已有分组里追加模型"时复用已保存的 API 地址与密钥。
func (db *DB) GroupCredentials(ctx context.Context, group string) (provider, apiURL, apiKey string, err error) {
	err = db.QueryRow(ctx, `SELECT provider, api_url, api_key FROM ai_connections
		WHERE group_name=$1 AND api_key <> '' ORDER BY updated_at DESC, id DESC LIMIT 1`, group).
		Scan(&provider, &apiURL, &apiKey)
	return provider, apiURL, apiKey, err
}

// AIGroupNames 返回库中实际存在的分组名。
func (db *DB) AIGroupNames(ctx context.Context) ([]string, error) {
	rows, err := db.Query(ctx, `SELECT DISTINCT group_name FROM ai_connections ORDER BY group_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
