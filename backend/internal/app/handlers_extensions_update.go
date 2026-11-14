package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"fanke/backend/internal/pluginrpc"
	"fanke/backend/internal/repo"
)

// ApplyExtensionUpdate 在线更新插件:下载清单里的 zip、校验 sha256、替换插件目录。
// 保留插件的 enabled/initialized 状态与 ext_data_* 数据表,不执行卸载。
func (a *App) ApplyExtensionUpdate(w http.ResponseWriter, r *http.Request) {
	e, err := a.DB.ExtensionBySlug(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		a.fail(w, http.StatusNotFound, "插件不存在")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	m, err := a.fetchManifest(ctx)
	if err != nil {
		a.fail(w, http.StatusBadGateway, "检查更新失败: "+err.Error())
		return
	}
	entry, ok := m.Plugins[e.Slug]
	if !ok {
		a.fail(w, http.StatusNotFound, "更新清单中没有该插件")
		return
	}
	if entry.Version == "" || entry.Version == e.Version {
		a.fail(w, http.StatusBadRequest, "已是最新版本")
		return
	}
	if entry.DownloadURL == "" {
		a.fail(w, http.StatusBadRequest, "清单缺少下载地址")
		return
	}
	if entry.SHA256 == "" {
		a.fail(w, http.StatusBadRequest, "清单缺少 sha256,已拒绝更新")
		return
	}

	tmp, err := os.MkdirTemp("", "fanke-ext-*")
	if err != nil {
		a.failInternal(w, r, "tmp", err)
		return
	}
	defer os.RemoveAll(tmp)
	pkg := filepath.Join(tmp, "pkg.zip")
	got, err := downloadToFile(ctx, entry.DownloadURL, pkg)
	if err != nil {
		a.fail(w, http.StatusBadGateway, "下载插件包失败: "+err.Error())
		return
	}
	if !strings.EqualFold(got, entry.SHA256) {
		a.fail(w, http.StatusBadRequest, "插件包 sha256 校验失败")
		return
	}
	data, err := os.ReadFile(pkg)
	if err != nil {
		a.failInternal(w, r, "read pkg", err)
		return
	}
	if err := a.installExtensionUpdate(e.ID, e.Slug, data); err != nil {
		a.fail(w, http.StatusBadRequest, "更新失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": entry.Version})
}

// installExtensionUpdate 解包、校验并把文件替换进插件目录,最后更新数据库元信息。
func (a *App) installExtensionUpdate(id int64, slug string, data []byte) error {
	files, err := readZip(data)
	if err != nil {
		return err
	}
	byName := map[string][]byte{}
	for _, fe := range files {
		byName[strings.ReplaceAll(fe.Name, "\\", "/")] = fe.Data
	}
	if err := compileExtensionSources(byName); err != nil {
		return errors.New("TypeScript 编译失败: " + err.Error())
	}
	root := ""
	for name := range byName {
		if path.Base(name) == "info.json" {
			dir := path.Dir(name)
			if dir == "." {
				dir = ""
			}
			if root == "" || len(dir) < len(root) {
				root = dir
			}
		}
	}
	prefix := ""
	if root != "" {
		prefix = root + "/"
	}
	infoRaw, ok := byName[prefix+"info.json"]
	if !ok {
		return errors.New("包内缺少 info.json")
	}
	infoRaw = []byte(strings.TrimPrefix(string(infoRaw), "\xef\xbb\xbf"))
	var info extInfo
	if err := json.Unmarshal(infoRaw, &info); err != nil {
		return errors.New("info.json 不是合法 JSON")
	}
	if strings.TrimSpace(info.Name) == "" || strings.TrimSpace(info.Author) == "" ||
		strings.TrimSpace(info.Version) == "" || strings.TrimSpace(info.Date) == "" {
		return errors.New("info.json 缺少必填字段(name/author/version/date)")
	}
	if info.Slug != "" && info.Slug != slug {
		return errors.New("更新包的 slug 与已安装插件不一致")
	}
	if info.Request == nil {
		info.Request = []string{}
	}
	if missing := ValidateHooks(info.Request); len(missing) > 0 {
		return errors.New("更新包声明了当前后端不支持的 hook: " + strings.Join(missing, ", "))
	}
	if _, ok := byName[prefix+"index.html"]; !ok {
		return errors.New("包内缺少 index.html")
	}
	if _, ok := byName[prefix+"init.js"]; !ok {
		if _, ok := byName[prefix+"init.ts"]; !ok {
			return errors.New("包内缺少 init.js 或 init.ts")
		}
	}

	dir := a.extDir(slug)
	stage := dir + ".new"
	backup := dir + ".bak"
	_ = os.RemoveAll(stage)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return err
	}
	for name, b := range byName {
		if prefix != "" {
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			name = strings.TrimPrefix(name, prefix)
		}
		dest, err := safeJoin(stage, name)
		if err != nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			continue
		}
		mode := os.FileMode(0o644)
		if strings.HasPrefix(name, "server/bin/") && strings.HasSuffix(name, "-server") {
			mode = 0o755
		}
		if err := os.WriteFile(dest, b, mode); err != nil {
			_ = os.RemoveAll(stage)
			return err
		}
	}
	if _, err := pluginrpc.LoadManifest(stage); err == nil {
		if err := pluginrpc.ValidateManifest(stage, slug, false); err != nil {
			_ = os.RemoveAll(stage)
			return errors.New("server manifest invalid: " + err.Error())
		}
	}
	_ = os.RemoveAll(backup)
	if err := os.Rename(dir, backup); err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	if err := os.Rename(stage, dir); err != nil {
		_ = os.Rename(backup, dir)
		_ = os.RemoveAll(stage)
		return err
	}
	_ = os.RemoveAll(backup)

	e := repo.Extension{
		Slug: slug, Name: info.Name, Author: info.Author, Version: info.Version, Date: info.Date,
		Description: info.Description, URL: info.URL, UpdateURL: info.UpdateURL, Favico: info.Favico,
		Hooks: info.Request,
	}
	if err := a.DB.UpdateExtensionMeta(context.Background(), id, e); err != nil {
		return errors.New("插件文件已更新,但写数据库失败: " + err.Error())
	}
	a.publicCache.clear()
	return nil
}
