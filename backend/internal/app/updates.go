package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"fanke/backend/internal/buildinfo"
)

// updateProxyURL 可选的更新代理(设置 UPDATE_PROXY);空则直连。
var updateProxyURL string

// ConfigureUpdateProxy 由 app.New 在启动时调用。
func ConfigureUpdateProxy(proxy string) {
	updateProxyURL = strings.TrimSpace(proxy)
}

// DefaultUpdateManifestURL 更新清单默认地址(GitHub raw,无需代理即可访问)。
// 如需 jsDelivr 镜像,设置 UPDATE_MANIFEST_URL 覆盖即可。
const DefaultUpdateManifestURL = "https://raw.githubusercontent.com/iammsf/fanke/main/updates.json"

const manifestMaxBytes = 1 << 20 // 清单上限 1MB

// updateEntry 清单中单个产品(本体或插件)的更新信息。
type updateEntry struct {
	Version     string `json:"version"`
	Date        string `json:"date"`
	Notes       string `json:"notes"`
	PageURL     string `json:"page_url"`
	DownloadURL string `json:"download_url"`
	// DownloadURLs / SHA256s 按 GOARCH 区分(amd64/arm64);新版本清单使用这两个字段,
	// DownloadURL / SHA256 保留做单架构兼容。
	DownloadURLs map[string]string `json:"download_urls"`
	SHA256       string            `json:"sha256"`
	SHA256s      map[string]string `json:"sha256s"`
}

// updateManifest 对应仓库根目录的 updates.json:
//
//	{
//	  "core":    { "version": "1.1.0", ... },
//	  "plugins": { "image-viewer": { "version": "1.6", ... } }
//	}
type updateManifest struct {
	Core    updateEntry            `json:"core"`
	Plugins map[string]updateEntry `json:"plugins"`
	Themes  map[string]updateEntry `json:"themes"`
}

// manifestURL 返回生效的清单地址(配置优先于默认值)。
func (a *App) manifestURL() string {
	if u := strings.TrimSpace(a.Cfg.UpdateManifestURL); u != "" {
		return u
	}
	return DefaultUpdateManifestURL
}

// curlToFile 用系统 curl 抓取(支持 --proxy、重定向、超时与体积上限)。
// 国内服务器直连 github.com 常超时,curl 可复用系统代理;失败时调用方再回落 Go。
func curlToFile(ctx context.Context, url, dest string, max int64) error {
	curl, err := exec.LookPath("curl")
	if err != nil {
		return err
	}
	args := []string{
		"-fsSL", "--retry", "2", "--connect-timeout", "20", "--max-time", "900",
		"--max-filesize", strconv.FormatInt(max+1, 10), "-o", dest,
	}
	if updateProxyURL != "" {
		args = append(args, "--proxy", updateProxyURL)
	}
	args = append(args, url)
	out, err := exec.CommandContext(ctx, curl, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("curl: %v %s", err, strings.TrimSpace(string(out)))
	}
	st, err := os.Stat(dest)
	if err != nil {
		return err
	}
	if st.Size() > max {
		return errors.New("响应超过大小上限")
	}
	return nil
}

// fetchUpdateJSON 优先用 curl(可走代理),失败再回落到 Go 探测客户端。
func fetchUpdateJSON(ctx context.Context, url string) ([]byte, error) {
	if err := validProbeURL(url); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "fanke-manifest-*")
	if err == nil {
		name := tmp.Name()
		_ = tmp.Close()
		defer os.Remove(name)
		if err := curlToFile(ctx, url, name, manifestMaxBytes); err == nil {
			if data, err := os.ReadFile(name); err == nil && len(data) <= manifestMaxBytes {
				return data, nil
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.New("更新地址非法")
	}
	req.Header.Set("User-Agent", "fanke-updater/1.0")
	resp, err := probeHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("更新地址返回 %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, manifestMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > manifestMaxBytes {
		return nil, errors.New("更新清单过大")
	}
	return data, nil
}

// fetchManifest 读取更新清单。
func (a *App) fetchManifest(ctx context.Context) (*updateManifest, error) {
	data, err := fetchUpdateJSON(ctx, a.manifestURL())
	if err != nil {
		return nil, err
	}
	var m updateManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, errors.New("更新清单格式错误")
	}
	if m.Core.Version == "" && len(m.Plugins) == 0 {
		return nil, errors.New("更新清单内容为空")
	}
	return &m, nil
}

// coreVersion 当前构建版本。
func coreVersion() string {
	if v := strings.TrimSpace(buildinfo.Version); v != "" {
		return v
	}
	return "dev"
}

// versionGreater 判断 latest 是否比 current 新。
// 只比较点分数字前缀(v1.2.3 / 1.2.3-rc1 / 2026.11.02 均可);无法解析时返回 false。
func versionGreater(latest, current string) bool {
	l, ok1 := parseNumericVersion(latest)
	c, ok2 := parseNumericVersion(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < len(l) || i < len(c); i++ {
		var a, b int
		if i < len(l) {
			a = l[i]
		}
		if i < len(c) {
			b = c[i]
		}
		if a != b {
			return a > b
		}
	}
	return false
}

func parseNumericVersion(v string) ([]int, bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return nil, false
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}
