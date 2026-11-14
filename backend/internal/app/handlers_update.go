package app

import (
	"context"
	"net/http"
	"time"
)

// Version 返回当前后端版本。公开接口,便于排查部署版本。
func (a *App) Version(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": coreVersion()})
}

// CheckCoreUpdate 检查博客本体是否有新版本(管理员)。
// 只做检查:返回发布页/下载地址,实际升级仍由部署脚本完成,
// 这样插件目录 extensions/ 不会被覆盖。
func (a *App) CheckCoreUpdate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	m, err := a.fetchManifest(ctx)
	if err != nil {
		a.fail(w, http.StatusBadGateway, "检查更新失败: "+err.Error())
		return
	}
	current := coreVersion()
	latest := m.Core.Version
	has := latest != "" && latest != current && versionGreater(latest, current)
	writeJSON(w, http.StatusOK, map[string]any{
		"current":      current,
		"latest":       latest,
		"date":         m.Core.Date,
		"notes":        m.Core.Notes,
		"page_url":     m.Core.PageURL,
		"download_url": m.Core.DownloadURL,
		"has_update":   has,
		"manifest_url": a.manifestURL(),
	})
}
