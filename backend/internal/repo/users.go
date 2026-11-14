package repo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrNotFound = errors.New("not found")

const userCols = `id, email, username, password_hash, role, display_name, avatar_url, bio, failed_attempts, locked_until, created_at, pwd_changed_at`

func scanUser(row pgx.Row) (*User, error) {
	u := &User{}
	err := row.Scan(&u.ID, &u.Email, &u.Username, &u.PasswordHash, &u.Role, &u.DisplayName,
		&u.AvatarURL, &u.Bio, &u.FailedCount, &u.LockedUntil, &u.CreatedAt, &u.PwdChangedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (db *DB) UserByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(db.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id=$1`, id))
}

// UserByEmailOrName 登录标识查找:仅匹配 ID(username,唯一)或邮箱;
// 昵称(display_name)可重复,不参与登录。
func (db *DB) UserByEmailOrName(ctx context.Context, ident string) (*User, error) {
	return scanUser(db.QueryRow(ctx,
		`SELECT `+userCols+` FROM users WHERE email=$1 OR username=$1`, ident))
}

func (db *DB) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := db.Query(ctx, `SELECT `+userCols+` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Username, &u.PasswordHash, &u.Role, &u.DisplayName,
			&u.AvatarURL, &u.Bio, &u.FailedCount, &u.LockedUntil, &u.CreatedAt, &u.PwdChangedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// AdminDisplayName 取第一位管理员的昵称(公开设置注入用);无管理员时返回空串。
func (db *DB) AdminDisplayName(ctx context.Context) (string, error) {
	var name string
	err := db.QueryRow(ctx, `SELECT display_name FROM users WHERE role='admin' ORDER BY id LIMIT 1`).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return name, err
}

func (db *DB) CreateUser(ctx context.Context, email, username, hash, role, displayName string) (int64, error) {
	var id int64
	err := db.QueryRow(ctx, `INSERT INTO users(email,username,password_hash,role,display_name)
		VALUES($1,$2,$3,$4,$5) RETURNING id`, email, username, hash, role, displayName).Scan(&id)
	return id, err
}

func (db *DB) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := db.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

func (db *DB) UpdateUserPassword(ctx context.Context, id int64, hash string) error {
	_, err := db.Exec(ctx, `UPDATE users SET password_hash=$2, failed_attempts=0, locked_until=NULL, pwd_changed_at=now() WHERE id=$1`, id, hash)
	return err
}

// RevokeUserSessions 吊销该用户全部 refresh token(access token 由 pwd_changed_at 判定失效)。
func (db *DB) RevokeUserSessions(ctx context.Context, id int64) error {
	_, err := db.Exec(ctx, `UPDATE refresh_tokens SET revoked=TRUE WHERE user_id=$1`, id)
	return err
}

// UpdateUser 修改用户名/邮箱/昵称/角色/头像/简介(空字符串字段不更新,bio 传指针可显式清空)。
func (db *DB) UpdateUser(ctx context.Context, id int64, email, username, displayName, role, avatar string, bio *string) error {
	_, err := db.Exec(ctx, `UPDATE users SET
		email=COALESCE(NULLIF($2,''),email),
		username=COALESCE(NULLIF($3,''),username),
		display_name=COALESCE(NULLIF($4,''),display_name),
		role=COALESCE(NULLIF($5,''),role),
		avatar_url=CASE WHEN $6='' THEN avatar_url ELSE $6 END,
		bio=CASE WHEN $7::text IS NULL THEN bio ELSE $7::text END
		WHERE id=$1`, id, email, username, displayName, role, avatar, bio)
	return err
}

func (db *DB) DeleteUser(ctx context.Context, id int64) error {
	_, err := db.Exec(ctx, `DELETE FROM users WHERE id=$1`, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// RecordLoginFailure 累加失败次数,连续 5 次锁定 15 分钟。
func (db *DB) RecordLoginFailure(ctx context.Context, id int64) error {
	_, err := db.Exec(ctx, `UPDATE users SET failed_attempts = failed_attempts + 1,
		locked_until = CASE WHEN failed_attempts + 1 >= 5 THEN now() + interval '15 minutes' ELSE locked_until END
		WHERE id=$1`, id)
	return err
}

func (db *DB) ClearLoginFailure(ctx context.Context, id int64) error {
	_, err := db.Exec(ctx, `UPDATE users SET failed_attempts=0, locked_until=NULL WHERE id=$1`, id)
	return err
}

func (db *DB) SaveRefreshToken(ctx context.Context, jti string, userID int64, ttl time.Duration) error {
	_, err := db.Exec(ctx, `INSERT INTO refresh_tokens(jti,user_id,expires_at) VALUES($1,$2,$3)`,
		jti, userID, time.Now().Add(ttl))
	return err
}

func (db *DB) ConsumeRefreshToken(ctx context.Context, jti string) (int64, error) {
	var uid int64
	err := db.inTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`SELECT user_id FROM refresh_tokens WHERE jti=$1 AND NOT revoked AND expires_at > now() FOR UPDATE`, jti,
		).Scan(&uid); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked=TRUE WHERE jti=$1`, jti)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return uid, err
}

func (db *DB) RevokeRefreshToken(ctx context.Context, jti string) error {
	_, err := db.Exec(ctx, `UPDATE refresh_tokens SET revoked=TRUE WHERE jti=$1`, jti)
	return err
}
