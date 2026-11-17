package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	"fanke/backend/internal/pluginrpc"
	"fanke/backend/internal/repo"
)

// maxBulkIDs 单次批量操作的条目上限,与媒体批量(maxBatchMedia)保持一致。
const maxBulkIDs = 500

// rejectedError 表示业务规则拒绝(生命周期钩子否决、前置条件不满足),
// 消息可直接回给前端;调用方按 422/409 处理而不是 500。
type rejectedError struct{ msg string }

func (e rejectedError) Error() string { return e.msg }

// bulkResult 批量结果:逐项容错,失败项带 id/slug 与原因,前端直接展示。
type bulkResult struct {
	OK     int      `json:"ok"`
	Failed []string `json:"failed"`
}

func newBulkResult() bulkResult { return bulkResult{Failed: []string{}} }

type bulkIDsReq struct {
	Action   string  `json:"action"`
	IDs      []int64 `json:"ids"`
	IsPublic bool    `json:"is_public"`
	Sticky   bool    `json:"sticky"`
	Status   string  `json:"status"`
}

// decodeBulkIDs 解析并校验批量请求体:ids 非空、不超上限、action 在白名单内。
func (a *App) decodeBulkIDs(w http.ResponseWriter, r *http.Request, req *bulkIDsReq, actions ...string) bool {
	if err := decodeJSON(r, req); err != nil || len(req.IDs) == 0 {
		a.fail(w, http.StatusBadRequest, "ids required")
		return false
	}
	if len(req.IDs) > maxBulkIDs {
		a.fail(w, http.StatusBadRequest, fmt.Sprintf("一次最多操作 %d 项", maxBulkIDs))
		return false
	}
	for _, act := range actions {
		if req.Action == act {
			return true
		}
	}
	a.fail(w, http.StatusBadRequest, "不支持的操作")
	return false
}

// BulkPosts 文章批量操作。publish/public/sticky 是纯 SQL,一条语句搞定;
// delete 必须逐项走,才能保留 beforePostDelete / afterPostDeleted 生命周期分发。
func (a *App) BulkPosts(w http.ResponseWriter, r *http.Request) {
	var req bulkIDsReq
	if !a.decodeBulkIDs(w, r, &req, "publish", "public", "sticky", "delete") {
		return
	}
	ctx := r.Context()
	res := newBulkResult()
	// 逐项容错:非自己的文章跳过并记入 failed,不整批拒绝。
	// 必须在 deletePostWithHooks 之前过滤——它被单条 DeletePost 共用,自身不做归属校验。
	ids := req.IDs
	if u := currentUser(r); u.Role != "admin" {
		owned := make([]int64, 0, len(req.IDs))
		for _, id := range req.IDs {
			p, err := a.DB.PostByID(ctx, id)
			if errors.Is(err, repo.ErrNotFound) {
				res.Failed = append(res.Failed, fmt.Sprintf("#%d: 文章不存在", id))
				continue
			}
			if err != nil {
				a.failInternal(w, r, "bulk check posts", err)
				return
			}
			if !canEditPost(u, p.AuthorID) {
				res.Failed = append(res.Failed, fmt.Sprintf("#%d: 只能操作自己的文章", id))
				continue
			}
			owned = append(owned, id)
		}
		ids = owned
	}
	switch req.Action {
	case "publish":
		n, err := a.DB.BulkPublishPosts(ctx, ids)
		if err != nil {
			a.failInternal(w, r, "bulk publish posts", err)
			return
		}
		res.OK = int(n)
	case "public":
		n, err := a.DB.BulkSetPostsPublic(ctx, ids, req.IsPublic)
		if err != nil {
			a.failInternal(w, r, "bulk set posts public", err)
			return
		}
		res.OK = int(n)
	case "sticky":
		n, err := a.DB.BulkSetPostsSticky(ctx, ids, req.Sticky)
		if err != nil {
			a.failInternal(w, r, "bulk set posts sticky", err)
			return
		}
		res.OK = int(n)
	case "delete":
		for _, id := range ids {
			if err := a.deletePostWithHooks(ctx, id); err != nil {
				res.Failed = append(res.Failed, fmt.Sprintf("#%d: %v", id, err))
				continue
			}
			res.OK++
		}
	}
	writeJSON(w, http.StatusOK, res)
}

// deletePostWithHooks 删除单篇文章并派发生命周期钩子;Before 被否决时返回 rejectedError。
func (a *App) deletePostWithHooks(ctx context.Context, id int64) error {
	old, err := a.DB.PostByID(ctx, id)
	if err != nil {
		return err
	}
	full, err := a.DB.PostBySlug(ctx, old.Slug, false, false)
	if err != nil {
		return err
	}
	data := &LifecycleData{PostID: id, OldPost: full, ContentHTML: full.ContentHTML}
	if err := a.dispatchLifecycle(ctx, BeforePostDelete, data); err != nil {
		return rejectedError{err.Error()}
	}
	if err := a.DB.DeletePost(ctx, id); err != nil {
		return err
	}
	_ = a.dispatchLifecycle(ctx, AfterPostDeleted, data)
	return nil
}

// BulkComments 评论批量操作;两类动作都是纯 SQL,无生命周期钩子。
func (a *App) BulkComments(w http.ResponseWriter, r *http.Request) {
	var req bulkIDsReq
	if !a.decodeBulkIDs(w, r, &req, "status", "delete") {
		return
	}
	if req.Action == "status" &&
		req.Status != "approved" && req.Status != "pending" && req.Status != "spam" {
		a.fail(w, http.StatusBadRequest, "status must be approved/pending/spam")
		return
	}
	ctx := r.Context()
	var n int64
	var err error
	if req.Action == "status" {
		n, err = a.DB.BulkSetCommentStatus(ctx, req.IDs, req.Status)
	} else {
		n, err = a.DB.BulkDeleteComments(ctx, req.IDs)
	}
	if err != nil {
		a.failInternal(w, r, "bulk comments", err)
		return
	}
	writeJSON(w, http.StatusOK, bulkResult{OK: int(n), Failed: []string{}})
}

// BulkUsers 用户批量删除。逐项走单条删除的守卫(不能删自己),
// 并预检外键引用,把 NO ACTION 冲突翻译成可读原因而不是 500。
func (a *App) BulkUsers(w http.ResponseWriter, r *http.Request) {
	var req bulkIDsReq
	if !a.decodeBulkIDs(w, r, &req, "delete") {
		return
	}
	ctx := r.Context()
	me := currentUser(r).ID
	blocked, err := a.DB.UserDeleteBlockers(ctx, req.IDs)
	if err != nil {
		a.failInternal(w, r, "bulk check users", err)
		return
	}
	res := newBulkResult()
	changed := false
	for _, id := range req.IDs {
		switch {
		case id == me:
			res.Failed = append(res.Failed, fmt.Sprintf("#%d: 不能删除自己", id))
		case blocked[id] != "":
			res.Failed = append(res.Failed, fmt.Sprintf("#%d: %s", id, blocked[id]))
		default:
			if err := a.DB.DeleteUser(ctx, id); err != nil {
				if errors.Is(err, repo.ErrNotFound) {
					res.Failed = append(res.Failed, fmt.Sprintf("#%d: 用户不存在", id))
					continue
				}
				res.Failed = append(res.Failed, fmt.Sprintf("#%d: %v", id, err))
				continue
			}
			res.OK++
			changed = true
		}
	}
	if changed {
		a.publicCache.clear()
	}
	writeJSON(w, http.StatusOK, res)
}

type bulkSlugsReq struct {
	Action  string   `json:"action"`
	Slugs   []string `json:"slugs"`
	Enabled bool     `json:"enabled"`
}

// BulkExtensions 插件批量启用/停用/卸载。按 slug 寻址(与单条路由一致),
// 逐项复用单条路径的前置校验,末尾只清一次公开缓存。
func (a *App) BulkExtensions(w http.ResponseWriter, r *http.Request) {
	var req bulkSlugsReq
	if err := decodeJSON(r, &req); err != nil || len(req.Slugs) == 0 {
		a.fail(w, http.StatusBadRequest, "slugs required")
		return
	}
	if len(req.Slugs) > maxBulkIDs {
		a.fail(w, http.StatusBadRequest, fmt.Sprintf("一次最多操作 %d 项", maxBulkIDs))
		return
	}
	if req.Action != "enabled" && req.Action != "uninstall" {
		a.fail(w, http.StatusBadRequest, "不支持的操作")
		return
	}
	ctx := r.Context()
	res := newBulkResult()
	changed := false
	for _, slug := range req.Slugs {
		e, err := a.DB.ExtensionBySlug(ctx, slug)
		if errors.Is(err, repo.ErrNotFound) {
			res.Failed = append(res.Failed, slug+": 插件不存在")
			continue
		}
		if err != nil {
			res.Failed = append(res.Failed, fmt.Sprintf("%s: %v", slug, err))
			continue
		}
		if req.Action == "enabled" {
			err = a.setExtensionEnabled(ctx, e, req.Enabled)
		} else {
			err = a.uninstallExtension(ctx, e)
		}
		if err != nil {
			res.Failed = append(res.Failed, fmt.Sprintf("%s: %v", slug, err))
			continue
		}
		res.OK++
		changed = true
	}
	if changed {
		a.publicCache.clear()
	}
	writeJSON(w, http.StatusOK, res)
}

// setExtensionEnabled 切换启用状态;启用前校验初始化完成与服务端 manifest。
func (a *App) setExtensionEnabled(ctx context.Context, e *repo.Extension, enabled bool) error {
	if enabled {
		if !e.Initialized {
			return rejectedError{"插件尚未完成初始化,请先在详情页执行 init"}
		}
		if err := pluginrpc.ValidateManifest(a.extDir(e.Slug), e.Slug, true); err != nil {
			return rejectedError{"可信服务端插件未通过启用检查: " + err.Error()}
		}
	}
	return a.DB.SetExtensionEnabled(ctx, e.ID, enabled)
}

// uninstallExtension 删除插件记录并清理目录(不触碰标签回落账本,回落要在卸载后继续生效)。
func (a *App) uninstallExtension(ctx context.Context, e *repo.Extension) error {
	if e.Slug == "s3-storage" {
		blocked, err := a.DB.HasS3MediaReferences(ctx)
		if err != nil {
			return err
		}
		if blocked {
			return rejectedError{"S3 媒体或派生图仍有引用，不能删除"}
		}
	}
	if err := a.DB.DeleteExtension(ctx, e.ID); err != nil {
		return err
	}
	_ = os.RemoveAll(a.extDir(e.Slug))
	return nil
}
