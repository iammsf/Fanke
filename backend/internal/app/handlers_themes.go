package app

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
)

var themeNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

func (a *App) themesDir() string { return a.Cfg.ThemesDir }

func (a *App) themePath(name string) (string, error) {
	if !themeNameRe.MatchString(name) {
		return "", errors.New("bad theme name")
	}
	p := filepath.Join(a.themesDir(), name, "tokens.css")
	if !strings.HasPrefix(p, filepath.Clean(a.themesDir())) {
		return "", errors.New("bad theme path")
	}
	return p, nil
}

// ListThemes 公开:运行时主题名列表(内置主题由前端编译期自带,不在此列)。
func (a *App) ListThemes(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(a.themesDir())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"names": []string{}})
		return
	}
	names := []string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(a.themesDir(), e.Name(), "tokens.css")); err == nil {
			names = append(names, e.Name())
		}
	}
	writeJSONCached(w, r, http.StatusOK, map[string]any{"names": names})
}

// GetThemeCSS 公开:返回主题 tokens.css 原文。
func (a *App) GetThemeCSS(w http.ResponseWriter, r *http.Request) {
	p, err := a.themePath(chi.URLParam(r, "name"))
	if err != nil {
		a.fail(w, http.StatusBadRequest, "bad theme name")
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

// UploadTheme 管理端:上传 .css(文件名即主题名)或 .zip(内含 tokens.css)。
func (a *App) UploadTheme(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		a.fail(w, http.StatusBadRequest, "invalid multipart")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		a.fail(w, http.StatusBadRequest, "file required")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 8<<20))
	if err != nil {
		a.fail(w, http.StatusRequestEntityTooLarge, "too large")
		return
	}

	var name, css string
	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	switch ext {
	case ".css":
		name = strings.TrimSuffix(filepath.Base(hdr.Filename), filepath.Ext(hdr.Filename))
		css = string(data)
	case ".zip":
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			a.fail(w, http.StatusBadRequest, "bad zip")
			return
		}
		for _, zf := range zr.File {
			base := strings.ToLower(filepath.Base(zf.Name))
			if base == "tokens.css" {
				rc, err := zf.Open()
				if err != nil {
					a.fail(w, http.StatusBadRequest, "zip read failed")
					return
				}
				b, _ := io.ReadAll(io.LimitReader(rc, 4<<20))
				rc.Close()
				css = string(b)
				// 主题名取 zip 内一级目录或 zip 文件名
				dir := strings.Split(filepath.ToSlash(zf.Name), "/")
				if len(dir) > 1 && dir[0] != "." && dir[0] != "" {
					name = dir[0]
				} else {
					name = strings.TrimSuffix(filepath.Base(hdr.Filename), ".zip")
				}
				break
			}
		}
		if css == "" {
			a.fail(w, http.StatusBadRequest, "zip 内未找到 tokens.css")
			return
		}
	default:
		a.fail(w, http.StatusBadRequest, "仅支持 .css 或 .zip")
		return
	}

	name = strings.ToLower(name)
	if !themeNameRe.MatchString(name) {
		a.fail(w, http.StatusBadRequest, "主题名只允许小写字母/数字/-/_,且以字母数字开头")
		return
	}
	dir := filepath.Join(a.themesDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.failInternal(w, r, "mkdir theme", err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "tokens.css"), []byte(css), 0o644); err != nil {
		a.failInternal(w, r, "write theme", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"name": name})
}

// DeleteTheme 管理端:删除运行时主题(内置主题不受影响)。
func (a *App) DeleteTheme(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !themeNameRe.MatchString(name) {
		a.fail(w, http.StatusBadRequest, "bad theme name")
		return
	}
	if err := os.RemoveAll(filepath.Join(a.themesDir(), name)); err != nil {
		a.failInternal(w, r, "delete theme", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
