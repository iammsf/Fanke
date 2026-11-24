package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

var themeNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

const (
	themePackageMax = 20 << 20
	themeFileMax    = 5 << 20
	themeFilesMax   = 50
)

// themeInfo 主题包 info.json。targets 声明这套主题用于文章页(site)、管理页(admin)或两者。
type themeInfo struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Date        string   `json:"date"`
	Author      string   `json:"author"`
	Description string   `json:"description"`
	Targets     []string `json:"targets"`
	UpdateURL   string   `json:"update_url"`
}

func (a *App) themesDir() string { return a.Cfg.ThemesDir }

func (a *App) themeDir(name string) (string, error) {
	if !themeNameRe.MatchString(name) {
		return "", errors.New("bad theme name")
	}
	dir := filepath.Join(a.themesDir(), name)
	if !strings.HasPrefix(dir, filepath.Clean(a.themesDir())+string(os.PathSeparator)) {
		return "", errors.New("bad theme path")
	}
	return dir, nil
}

func themeHasTarget(info themeInfo, target string) bool {
	for _, t := range info.Targets {
		if t == target {
			return true
		}
	}
	return false
}

// themeCSSPath 返回某个区域实际生效的 CSS:优先 site.css/admin.css,旧主题退回 tokens.css。
func (a *App) themeCSSPath(name, target string) (string, error) {
	dir, err := a.themeDir(name)
	if err != nil {
		return "", err
	}
	preferred := filepath.Join(dir, target+".css")
	if _, err := os.Stat(preferred); err == nil {
		return preferred, nil
	}
	legacy := filepath.Join(dir, "tokens.css")
	if _, err := os.Stat(legacy); err == nil {
		return legacy, nil
	}
	return "", errors.New("theme css not found")
}

func normalizeTargets(targets []string, hasSite, hasAdmin, hasTokens bool) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range targets {
		t = strings.ToLower(strings.TrimSpace(t))
		if (t == "site" || t == "admin") && !seen[t] {
			out = append(out, t)
			seen[t] = true
		}
	}
	if len(out) == 0 {
		if hasTokens {
			out = []string{"site", "admin"}
		} else {
			if hasSite {
				out = append(out, "site")
			}
			if hasAdmin {
				out = append(out, "admin")
			}
		}
	}
	sort.Strings(out)
	return out
}

// loadTheme 读取主题元信息;旧目录只有 tokens.css 时按双端主题处理。
func (a *App) loadTheme(name string) (themeInfo, error) {
	dir, err := a.themeDir(name)
	if err != nil {
		return themeInfo{}, err
	}
	info := themeInfo{Slug: name, Name: name, Targets: []string{"site", "admin"}}
	if raw, err := os.ReadFile(filepath.Join(dir, "info.json")); err == nil {
		if err := json.Unmarshal(raw, &info); err != nil {
			return themeInfo{}, errors.New("info.json 格式错误")
		}
		info.Slug = name
		if strings.TrimSpace(info.Name) == "" {
			info.Name = name
		}
	}
	hasSite := fileExists(filepath.Join(dir, "site.css"))
	hasAdmin := fileExists(filepath.Join(dir, "admin.css"))
	hasTokens := fileExists(filepath.Join(dir, "tokens.css"))
	if !hasSite && !hasAdmin && !hasTokens {
		return themeInfo{}, errors.New("主题缺少 CSS 文件")
	}
	info.Targets = normalizeTargets(info.Targets, hasSite, hasAdmin, hasTokens)
	if len(info.Targets) == 0 {
		return themeInfo{}, errors.New("主题没有可用目标")
	}
	return info, nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func (a *App) listThemes() []themeInfo {
	entries, err := os.ReadDir(a.themesDir())
	if err != nil {
		return []themeInfo{}
	}
	out := []themeInfo{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := a.loadTheme(e.Name())
		if err != nil {
			continue
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

// ListThemes 公开:运行时主题列表(内置主题由前端编译期自带,不在此列)。
func (a *App) ListThemes(w http.ResponseWriter, r *http.Request) {
	items := a.listThemes()
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Slug)
	}
	writeJSONCached(w, r, http.StatusOK, map[string]any{"names": names, "items": items})
}

func (a *App) serveThemeCSS(w http.ResponseWriter, name, target string) {
	p, err := a.themeCSSPath(name, target)
	if err != nil {
		a.fail(w, http.StatusNotFound, "theme not found")
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		a.fail(w, http.StatusNotFound, "theme not found")
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

// GetThemeCSS 兼容旧接口:返回 site.css,没有则 tokens.css。
func (a *App) GetThemeCSS(w http.ResponseWriter, r *http.Request) {
	a.serveThemeCSS(w, chi.URLParam(r, "name"), "site")
}

// GetThemeSiteCSS 文章页主题。
func (a *App) GetThemeSiteCSS(w http.ResponseWriter, r *http.Request) {
	a.serveThemeCSS(w, chi.URLParam(r, "name"), "site")
}

// GetThemeAdminCSS 管理页主题。
func (a *App) GetThemeAdminCSS(w http.ResponseWriter, r *http.Request) {
	a.serveThemeCSS(w, chi.URLParam(r, "name"), "admin")
}

// readThemePackage 从 zip 中只取出 info.json / site.css / admin.css / tokens.css。
func readThemePackage(data []byte, zipName string) (themeInfo, map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return themeInfo{}, nil, errors.New("不是有效的 zip")
	}
	if len(zr.File) > themeFilesMax {
		return themeInfo{}, nil, errors.New("包内文件过多")
	}
	files := map[string][]byte{}
	var total int64
	var info themeInfo
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		if zf.FileInfo().Mode()&os.ModeSymlink != 0 {
			return themeInfo{}, nil, errors.New("包内不允许符号链接")
		}
		base := strings.ToLower(filepath.Base(zf.Name))
		switch base {
		case "info.json", "site.css", "admin.css", "tokens.css":
		default:
			continue
		}
		if zf.UncompressedSize64 > themeFileMax {
			return themeInfo{}, nil, fmt.Errorf("文件过大: %s", base)
		}
		total += int64(zf.UncompressedSize64)
		if total > themePackageMax {
			return themeInfo{}, nil, errors.New("主题包解压后过大")
		}
		rc, err := zf.Open()
		if err != nil {
			return themeInfo{}, nil, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, themeFileMax+1))
		rc.Close()
		if err != nil {
			return themeInfo{}, nil, err
		}
		files[base] = b
	}
	if raw, ok := files["info.json"]; ok {
		if err := json.Unmarshal(raw, &info); err != nil {
			return themeInfo{}, nil, errors.New("info.json 格式错误")
		}
	}
	if strings.TrimSpace(info.Slug) == "" {
		info.Slug = strings.ToLower(strings.TrimSuffix(zipName, filepath.Ext(zipName)))
	}
	info.Slug = strings.ToLower(strings.TrimSpace(info.Slug))
	if !themeNameRe.MatchString(info.Slug) {
		return themeInfo{}, nil, errors.New("主题 slug 只允许小写字母/数字/-/_,且以字母数字开头")
	}
	if strings.TrimSpace(info.Name) == "" {
		info.Name = info.Slug
	}
	_, hasSite := files["site.css"]
	_, hasAdmin := files["admin.css"]
	_, hasTokens := files["tokens.css"]
	info.Targets = normalizeTargets(info.Targets, hasSite, hasAdmin, hasTokens)
	if len(info.Targets) == 0 {
		return themeInfo{}, nil, errors.New("主题缺少 site.css / admin.css / tokens.css")
	}
	for _, t := range info.Targets {
		if t == "site" && !hasSite && !hasTokens {
			return themeInfo{}, nil, errors.New("声明了 site 目标,但缺少 site.css")
		}
		if t == "admin" && !hasAdmin && !hasTokens {
			return themeInfo{}, nil, errors.New("声明了 admin 目标,但缺少 admin.css")
		}
	}
	return info, files, nil
}

// writeTheme 以临时目录 + rename 方式写入/覆盖主题。
func (a *App) writeTheme(info themeInfo, files map[string][]byte) error {
	dir, err := a.themeDir(info.Slug)
	if err != nil {
		return err
	}
	tmp := dir + ".new"
	backup := dir + ".bak"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(info, "", "  ")
	raw = append(raw, '\n')
	if err := os.WriteFile(filepath.Join(tmp, "info.json"), raw, 0o644); err != nil {
		return err
	}
	for name, data := range files {
		if name == "info.json" {
			continue
		}
		if err := os.WriteFile(filepath.Join(tmp, name), data, 0o644); err != nil {
			return err
		}
	}
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(dir); err == nil {
		if err := os.Rename(dir, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		_ = os.Rename(backup, dir)
		_ = os.RemoveAll(tmp)
		return err
	}
	_ = os.RemoveAll(backup)
	a.publicCache.clear()
	return nil
}

// UploadTheme 管理端:上传 .css(旧式全局主题)或 .zip(info.json + site.css/admin.css)。
func (a *App) UploadTheme(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(themePackageMax); err != nil {
		a.fail(w, http.StatusBadRequest, "invalid multipart")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		a.fail(w, http.StatusBadRequest, "file required")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, themePackageMax+1))
	if err != nil || len(data) > themePackageMax {
		a.fail(w, http.StatusRequestEntityTooLarge, "主题包超过 20MB")
		return
	}
	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	var info themeInfo
	var files map[string][]byte
	switch ext {
	case ".css":
		slug := strings.ToLower(strings.TrimSuffix(filepath.Base(hdr.Filename), filepath.Ext(hdr.Filename)))
		info = themeInfo{Slug: slug, Name: slug, Targets: []string{"admin", "site"}}
		files = map[string][]byte{"tokens.css": data}
		if !themeNameRe.MatchString(slug) {
			a.fail(w, http.StatusBadRequest, "主题名只允许小写字母/数字/-/_,且以字母数字开头")
			return
		}
	case ".zip":
		info, files, err = readThemePackage(data, hdr.Filename)
		if err != nil {
			a.fail(w, http.StatusBadRequest, err.Error())
			return
		}
	default:
		a.fail(w, http.StatusBadRequest, "仅支持 .css 或 .zip")
		return
	}
	if err := a.writeTheme(info, files); err != nil {
		a.failInternal(w, r, "write theme", err)
		return
	}
	writeJSON(w, http.StatusCreated, info)
}

// DeleteTheme 管理端:删除运行时主题(内置主题不受影响)。
func (a *App) DeleteTheme(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	dir, err := a.themeDir(name)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "bad theme name")
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		a.failInternal(w, r, "delete theme", err)
		return
	}
	a.publicCache.clear()
	w.WriteHeader(http.StatusNoContent)
}

// CheckThemeUpdate 检查主题更新:优先用主题自己的 update_url,否则用共享清单的 themes 段。
func (a *App) CheckThemeUpdate(w http.ResponseWriter, r *http.Request) {
	info, err := a.loadTheme(chi.URLParam(r, "slug"))
	if err != nil {
		a.fail(w, http.StatusNotFound, "主题不存在")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	entry, err := a.findThemeEntry(ctx, info)
	if err != nil {
		a.fail(w, http.StatusBadGateway, "检查更新失败: "+err.Error())
		return
	}
	hasUpdate := entry.Version != "" && entry.Version != info.Version
	writeJSON(w, http.StatusOK, map[string]any{
		"version": entry.Version, "download_url": entry.DownloadURL,
		"page_url": entry.PageURL, "notes": entry.Notes, "date": entry.Date,
		"has_update": hasUpdate,
	})
}

// ApplyThemeUpdate 下载并安装主题更新包。
func (a *App) ApplyThemeUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.updateMu.TryLock() {
		a.fail(w, http.StatusConflict, "已有更新任务正在进行，请稍后重试")
		return
	}
	defer a.updateMu.Unlock()
	info, err := a.loadTheme(chi.URLParam(r, "slug"))
	if err != nil {
		a.fail(w, http.StatusNotFound, "主题不存在")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	entry, err := a.findThemeEntry(ctx, info)
	if err != nil {
		a.fail(w, http.StatusBadGateway, "检查更新失败: "+err.Error())
		return
	}
	if entry.Version == "" || entry.Version == info.Version {
		a.fail(w, http.StatusBadRequest, "已是最新版本")
		return
	}
	if entry.DownloadURL == "" {
		a.fail(w, http.StatusBadRequest, "主题清单缺少下载地址")
		return
	}
	if entry.SHA256 == "" {
		a.fail(w, http.StatusBadRequest, "主题清单缺少 sha256,已拒绝升级")
		return
	}
	tmp, err := os.MkdirTemp("", "fanke-theme-*")
	if err != nil {
		a.failInternal(w, r, "tmp", err)
		return
	}
	defer os.RemoveAll(tmp)
	pkg := filepath.Join(tmp, "theme.zip")
	got, err := downloadToFile(ctx, entry.DownloadURL, pkg)
	if err != nil {
		a.fail(w, http.StatusBadGateway, "下载主题包失败: "+err.Error())
		return
	}
	if !strings.EqualFold(got, entry.SHA256) {
		a.fail(w, http.StatusBadRequest, "主题包 sha256 校验失败")
		return
	}
	data, err := os.ReadFile(pkg)
	if err != nil {
		a.failInternal(w, r, "read pkg", err)
		return
	}
	newInfo, files, err := readThemePackage(data, info.Slug+".zip")
	if err != nil {
		a.fail(w, http.StatusBadRequest, "主题包无效: "+err.Error())
		return
	}
	newInfo.Slug = info.Slug
	if err := a.writeTheme(newInfo, files); err != nil {
		a.failInternal(w, r, "install theme", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": newInfo.Version})
}

func (a *App) findThemeEntry(ctx context.Context, info themeInfo) (updateEntry, error) {
	if u := strings.TrimSpace(info.UpdateURL); u != "" {
		data, err := fetchUpdateJSON(ctx, u)
		if err != nil {
			return updateEntry{}, err
		}
		return decodeThemeEntry(info.Slug, data)
	}
	m, err := a.fetchManifest(ctx)
	if err != nil {
		return updateEntry{}, err
	}
	entry, ok := m.Themes[info.Slug]
	if !ok {
		return updateEntry{}, errors.New("更新清单中没有该主题")
	}
	return entry, nil
}

func decodeThemeEntry(slug string, data []byte) (updateEntry, error) {
	var m updateManifest
	if err := json.Unmarshal(data, &m); err == nil {
		if entry, ok := m.Themes[slug]; ok {
			return entry, nil
		}
		if len(m.Themes) > 0 {
			return updateEntry{}, errors.New("更新清单中没有该主题")
		}
	}
	var flat updateEntry
	if err := json.Unmarshal(data, &flat); err == nil && flat.Version != "" {
		return flat, nil
	}
	return updateEntry{}, errors.New("更新地址返回格式错误")
}
