package repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (db *DB) CommentPostBySlug(ctx context.Context, slug string) (*Post, error) {
	out := &Post{}
	err := db.QueryRow(ctx, `SELECT id, author_id, status, is_public, allow_comments
		FROM posts WHERE slug=$1 AND status='published'`, slug).
		Scan(&out.ID, &out.AuthorID, &out.Status, &out.IsPublic, &out.AllowComments)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (db *DB) ListComments(ctx context.Context, postID int64, statuses []string) ([]Comment, error) {
	if len(statuses) == 0 {
		statuses = []string{"approved"}
	}
	rows, err := db.Query(ctx, `SELECT id, post_id, parent_id, author_name, author_email, user_id, content, status, created_at
		FROM comments WHERE post_id=$1 AND status = ANY($2) ORDER BY created_at`, postID, statuses)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.PostID, &c.ParentID, &c.AuthorName, &c.AuthorEmail, &c.UserID,
			&c.Content, &c.Status, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CommentParentExists 校验父评论属于同一篇文章且已通过审核(直接走主键/条件查询,不再全量拉取)。
func (db *DB) CommentParentExists(ctx context.Context, postID, parentID int64) (bool, error) {
	var ok bool
	err := db.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM comments WHERE id=$1 AND post_id=$2 AND status='approved')`,
		parentID, postID).Scan(&ok)
	return ok, err
}

// ListAllComments 管理端跨文章评论列表。
func (db *DB) ListAllComments(ctx context.Context, status string, limit, offset int) ([]Comment, int, error) {
	var total int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM comments WHERE ($1='' OR status=$1)`, status).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := db.Query(ctx, `SELECT c.id, c.post_id, c.parent_id, c.author_name, c.author_email, c.user_id, c.content, c.status, c.created_at
		FROM comments c WHERE ($1='' OR c.status=$1) ORDER BY c.created_at DESC LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.PostID, &c.ParentID, &c.AuthorName, &c.AuthorEmail, &c.UserID,
			&c.Content, &c.Status, &c.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

func (db *DB) CreateComment(ctx context.Context, c Comment) (int64, error) {
	var id int64
	if c.CreatedAt.IsZero() {
		err := db.QueryRow(ctx, `INSERT INTO comments(post_id,parent_id,author_name,author_email,user_id,content,status,ip_hash)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
			c.PostID, c.ParentID, c.AuthorName, c.AuthorEmail, c.UserID, c.Content, c.Status, c.IPHash).Scan(&id)
		return id, err
	}
	err := db.QueryRow(ctx, `INSERT INTO comments(post_id,parent_id,author_name,author_email,user_id,content,status,ip_hash,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		c.PostID, c.ParentID, c.AuthorName, c.AuthorEmail, c.UserID, c.Content, c.Status, c.IPHash, c.CreatedAt).Scan(&id)
	return id, err
}

func (db *DB) SetCommentStatus(ctx context.Context, id int64, status string) error {
	_, err := db.Exec(ctx, `UPDATE comments SET status=$2 WHERE id=$1`, id, status)
	return err
}

func (db *DB) DeleteComment(ctx context.Context, id int64) error {
	_, err := db.Exec(ctx, `DELETE FROM comments WHERE id=$1`, id)
	return err
}

// CommentCountForPost 公开文章页显示的已审核评论数。
func (db *DB) CommentCountForPost(ctx context.Context, postID int64) (int, error) {
	var n int
	err := db.QueryRow(ctx, `SELECT count(*) FROM comments WHERE post_id=$1 AND status='approved'`, postID).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

// BulkSetCommentStatus 批量改评论状态,单条 SQL 完成;返回实际更新行数。
func (db *DB) BulkSetCommentStatus(ctx context.Context, ids []int64, status string) (int64, error) {
	tag, err := db.Exec(ctx, `UPDATE comments SET status=$2 WHERE id = ANY($1)`, ids, status)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// BulkDeleteComments 批量删除评论;子评论由 comments.parent_id 自引用 CASCADE 一并清理。
func (db *DB) BulkDeleteComments(ctx context.Context, ids []int64) (int64, error) {
	tag, err := db.Exec(ctx, `DELETE FROM comments WHERE id = ANY($1)`, ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
