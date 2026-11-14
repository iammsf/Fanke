package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"fanke/backend/internal/buildinfo"
)

// DefaultUpdateManifestURL 更新清单默认地址(GitHub 仓库根目录的 updates.json)。
// 大陆网络访问 raw.githubusercontent.com 不稳定时,可用 UPDATE_MANIFEST_URL
// 指向 jsDelivr 镜像等可达地址,内容格式一致。
const DefaultUpdateManifestURL = "https://raw.githubusercontent.com/iammsf/fanke/main/updates.json"

const manifestMaxBytes = 1 << 20 // 清单上限 1MB

// updateEntry 清单中单个产品(本体或插件)的更新信息。
type updateEntry struct {
	Version     string `json:"version"`
	Date        string `json:"date"`
	Notes       string `json:"notes"`
	PageURL     string `json:"page_url"`
	DownloadURL string `json:"download_url"`
	SHA256      string `json:"sha256"`
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
}

// manifestURL 返回生效的清单地址(配置优先于默认值)。
func (a *App) manifestURL() string {
	if u := strings.TrimSpace(a.Cfg.UpdateManifestURL); u != "" {
		return u
	}
	return DefaultUpdateManifestURL
}

// fetchUpdateJSON 用探测客户端抓取公开 JSON:不走环境代理、拦截内网目标、限制体积。
func fetchUpdateJSON(ctx context.Context, url string) ([]byte, error) {
	if err := validProbeURL(url); err != nil {
		return nil, err
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
