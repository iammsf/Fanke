package app

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	updateArchiveMax = 300 << 20 // 升级包下载上限 300MB
	updateExtractMax = 500 << 20 // 解压后总量上限 500MB
	updateFileMax    = 5000      // 包内文件数上限
)

// updateHTTPClient 下载升级包专用:不走环境代理,连接阶段拦截内网,重定向逐跳校验。
var updateHTTPClient = &http.Client{
	Timeout: 15 * time.Minute,
	Transport: &http.Transport{
		Proxy:                 nil,
		DialContext:           safeProbeDial,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	},
	CheckRedirect: probeHTTPClient.CheckRedirect,
}

// updateTargets 解析安装目录与前端静态目录。
//
//	APP_DIR  默认从可执行文件推断(/opt/fanke/bin/api → /opt/fanke)
//	WEB_ROOT 默认取 THEMES_DIR 所在站点的 index 目录,否则 AppDir/web
func (a *App) updateTargets() (string, string, error) {
	appDir := strings.TrimSpace(a.Cfg.AppDir)
	if appDir == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", "", fmt.Errorf("无法定位可执行文件: %w", err)
		}
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		dir := filepath.Dir(exe)
		if filepath.Base(dir) == "bin" {
			appDir = filepath.Dir(dir)
		} else {
			appDir = dir
		}
	}
	webRoot := strings.TrimSpace(a.Cfg.WebRoot)
	if webRoot == "" {
		themes := filepath.Clean(a.Cfg.ThemesDir)
		siteRoot := filepath.Dir(themes)
		if filepath.Base(themes) == "themes" {
			candidate := filepath.Join(siteRoot, "index")
			if st, err := os.Stat(candidate); err == nil && st.IsDir() {
				webRoot = candidate
			}
		}
		if webRoot == "" {
			webRoot = filepath.Join(appDir, "web")
		}
	}
	if _, err := os.Stat(appDir); err != nil {
		return "", "", fmt.Errorf("安装目录不可用: %s", appDir)
	}
	if filepath.Clean(webRoot) == string(filepath.Separator) {
		return "", "", errors.New("WEB_ROOT 不能是根目录")
	}
	return appDir, webRoot, nil
}

// applyCoreUpdate 下载并安装本体升级包:
//   - 只替换 <appDir>/bin/api 与 WEB_ROOT 下的前端文件
//   - extensions/ themes/ media/ state/ 与数据库完全不动
//
// 返回执行结果,调用方负责写响应并让进程退出(由 systemd 拉起新二进制)。
func (a *App) applyCoreUpdate(ctx context.Context, m *updateManifest) (map[string]any, error) {
	current := coreVersion()
	latest := m.Core.Version
	if latest == "" || latest == current || !versionGreater(latest, current) {
		return nil, errors.New("没有可用的新版本")
	}
	arch := runtime.GOARCH
	url := ""
	if m.Core.DownloadURLs != nil {
		url = strings.TrimSpace(m.Core.DownloadURLs[arch])
	}
	if url == "" {
		url = strings.TrimSpace(m.Core.DownloadURL)
	}
	if url == "" {
		return nil, fmt.Errorf("更新清单缺少 %s 架构的下载地址", arch)
	}
	sum := ""
	if m.Core.SHA256s != nil {
		sum = strings.TrimSpace(m.Core.SHA256s[arch])
	}
	if sum == "" {
		sum = strings.TrimSpace(m.Core.SHA256)
	}
	if sum == "" {
		return nil, errors.New("更新清单缺少 sha256,已拒绝升级")
	}
	appDir, webRoot, err := a.updateTargets()
	if err != nil {
		return nil, err
	}

	tmp, err := os.MkdirTemp("", "fanke-update-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	archive := filepath.Join(tmp, "pkg.tar.gz")
	got, err := downloadToFile(ctx, url, archive)
	if err != nil {
		return nil, fmt.Errorf("下载升级包失败: %w", err)
	}
	if !strings.EqualFold(got, sum) {
		return nil, errors.New("升级包 sha256 校验失败,已中止")
	}

	unpack := filepath.Join(tmp, "unpack")
	if err := extractCoreArchive(archive, unpack); err != nil {
		return nil, err
	}
	if err := installBackend(appDir, filepath.Join(unpack, "bin", "api")); err != nil {
		return nil, err
	}
	if err := replaceWebRoot(filepath.Join(unpack, "web"), webRoot); err != nil {
		return nil, fmt.Errorf("更新前端失败: %w", err)
	}
	return map[string]any{
		"ok": true, "from": current, "to": latest, "restarting": true,
	}, nil
}

// downloadToFile 下载到文件并返回实际字节的 sha256(hex)。
// 优先用 curl:国内服务器直连 github.com 常超时,但系统 curl 可走本地代理。
func downloadToFile(ctx context.Context, url, dest string) (string, error) {
	if err := validProbeURL(url); err != nil {
		return "", err
	}
	if err := curlToFile(ctx, url, dest, updateArchiveMax); err == nil {
		return fileSHA256(dest)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "fanke-updater/1.0")
	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载地址返回 %d", resp.StatusCode)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, updateArchiveMax+1))
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if n > updateArchiveMax {
		return "", errors.New("升级包超过 300MB 上限")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// fileSHA256 计算文件哈希。
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractCoreArchive 只解出 bin/api 与 web/ 下的文件,忽略其他顶层内容。
func extractCoreArchive(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return errors.New("升级包不是有效的 tar.gz")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	var total int64
	count := 0
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.ToSlash(h.Name)
		parts := strings.SplitN(name, "/", 2)
		if len(parts) != 2 {
			continue // 顶层目录条目
		}
		rel := parts[1]
		if rel == "" {
			continue
		}
		rel = path.Clean(rel)
		// 只拦真正的路径穿越;前端产物里可能有 "_...xxx.css" 这类合法文件名。
		if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, "/") {
			return errors.New("升级包包含非法路径")
		}
		if rel != "bin/api" && !strings.HasPrefix(rel, "web/") {
			continue
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return errors.New("升级包包含非普通文件")
		}
		count++
		if count > updateFileMax {
			return errors.New("升级包文件数超过上限")
		}
		total += h.Size
		if total > updateExtractMax {
			return errors.New("升级包解压后体积超过上限")
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return errors.New("升级包路径越界")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if rel == "bin/api" {
			mode = 0o755
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(tr, h.Size))
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if count == 0 {
		return errors.New("升级包内容为空")
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "api")); err != nil {
		return errors.New("升级包缺少 bin/api")
	}
	if _, err := os.Stat(filepath.Join(dest, "web", "index.html")); err != nil {
		return errors.New("升级包缺少 web/index.html")
	}
	return nil
}

// installBackend 原子替换 bin/api,保留一份最近备份。
func installBackend(appDir, newAPI string) error {
	binDir := filepath.Join(appDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	staged := filepath.Join(binDir, ".api.new")
	if err := copyFile(newAPI, staged, 0o755); err != nil {
		return fmt.Errorf("写入新版本失败(检查服务账号对 %s 的写权限): %w", binDir, err)
	}
	target := filepath.Join(binDir, "api")
	backup := filepath.Join(binDir, "api.bak-"+time.Now().Format("20060102-150405"))
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			_ = os.Remove(staged)
			return err
		}
	}
	if err := os.Rename(staged, target); err != nil {
		_ = os.Rename(backup, target)
		_ = os.Remove(staged)
		return err
	}
	pruneBackups(binDir, "api.bak-", 2)
	return nil
}

// replaceWebRoot 用新构建的前端覆盖 WEB_ROOT(只清理该目录内部)。
func replaceWebRoot(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dst)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return copyTree(src, dst)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFile(path, target, 0o644)
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chmod(dst, mode)
}

// pruneBackups 只保留最近 keep 个以 prefix 开头的备份。
func pruneBackups(dir, prefix string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			names = append(names, e.Name())
		}
	}
	// 名称含时间戳,字典序即时间序
	sort.Strings(names)
	for i := 0; i < len(names)-keep; i++ {
		_ = os.Remove(filepath.Join(dir, names[i]))
	}
}
