package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"fanke/backend/internal/buildinfo"
	"fanke/backend/internal/netutil"
)

// updateProxyURL 可选的更新代理(设置 UPDATE_PROXY);空则直连。
var updateProxyURL string

// updateAllowInsecureHTTP 是否允许更新下载走明文 http://(由 ALLOW_INSECURE_UPDATE_HTTP=true 显式开启)。
var updateAllowInsecureHTTP bool

// ConfigureUpdateProxy 由 app.New 在启动时调用。
func ConfigureUpdateProxy(proxy string) {
	updateProxyURL = strings.TrimSpace(proxy)
}

// ConfigureUpdateHTTP 由 app.New 在启动时调用。
func ConfigureUpdateHTTP(allow bool) {
	updateAllowInsecureHTTP = allow
}

func allowInsecureUpdateHTTP() bool {
	if updateAllowInsecureHTTP {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(os.Getenv("ALLOW_INSECURE_UPDATE_HTTP")), "true")
}

// validUpdateURL 校验自更新/插件/主题下载地址:默认仅允许 https://(除非显式开启 ALLOW_INSECURE_UPDATE_HTTP=true),
// 同时拒绝字面量内网/回环/云元数据地址。
func validUpdateURL(raw string) error {
	_, err := netutil.ValidateURL(raw, "", !allowInsecureUpdateHTTP())
	return err
}

// updateManifestHTTPClient 更新清单回落客户端:不走环境代理,连接阶段拦截内网,重定向强制校验 validUpdateURL。
var updateManifestHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		Proxy:               nil,
		DialContext:         safeProbeDial,
		TLSHandshakeTimeout: 5 * time.Second,
		DisableKeepAlives:   true,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxUpdateRedirects {
			return errors.New("重定向次数过多")
		}
		return validUpdateURL(req.URL.String())
	},
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

// maxUpdateRedirects 与 probeHTTPClient 的跳数上限保持一致。
const maxUpdateRedirects = 4

// curlToFile 用系统 curl 抓取(支持 --proxy、超时与体积上限)。
// 国内服务器直连 github.com 常超时,curl 可复用系统代理;失败时调用方再回落 Go。
//
// 这里刻意不用 curl -L:-L 会无条件跟随 3xx,重定向后的地址没人校验,
// 一条 https 的外部地址就能把请求引到内网(SSRF)。改成手动逐跳:每跳先过
// validProbeURL,与已经加固过的 Go 探测路径口径一致。
func curlToFile(ctx context.Context, target, dest string, max int64) error {
	allowHTTP := allowInsecureUpdateHTTP()
	for hop := 0; ; hop++ {
		if err := validUpdateURL(target); err != nil {
			return err
		}
		resolveArg := ""
		if updateProxyURL == "" {
			// 先在 Go 侧解析域名并校验所有 IP 非内网/元数据,再用 --resolve 钉死连接 IP 防 DNS 重绑定 SSRF。
			host, port, safeIP, err := netutil.ResolveSafeIP(ctx, target, allowHTTP)
			if err != nil {
				return err
			}
			resolveArg = netutil.FormatCurlResolve(host, port, safeIP)
		} else {
			// 走代理时若本机 DNS 能解析出内网/回环 IP(如 localtest.me -> 127.0.0.1),同样直接拒绝。
			if u, err := url.Parse(target); err == nil {
				if ips, derr := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname()); derr == nil {
					for _, ip := range ips {
						if isForbiddenProbeIP(ip.IP) {
							return netutil.ErrForbiddenAddress
						}
					}
				}
			}
		}
		code, next, err := curlOnceOpts(ctx, target, dest, max, resolveArg, allowHTTP)
		if err != nil {
			return err
		}
		if code >= 300 && code <= 399 {
			if next == "" {
				return fmt.Errorf("curl: %d 重定向但缺少 Location", code)
			}
			if hop >= maxUpdateRedirects {
				return errors.New("重定向次数过多")
			}
			resolved, err := absoluteURL(target, next)
			if err != nil {
				return err
			}
			target = resolved
			continue
		}
		if code != http.StatusOK {
			return fmt.Errorf("curl: 返回 %d", code)
		}
		break
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

// curlOnce 抓一跳:不带 -L,状态码与下一跳地址用 -w 打到 stdout 由调用方裁决。
func curlOnce(ctx context.Context, target, dest string, max int64) (int, string, error) {
	return curlOnceOpts(ctx, target, dest, max, "", true)
}

func buildCurlArgs(target, dest string, max int64, resolveArg string, allowHTTP bool, proxy string) []string {
	proto := "=https"
	if allowHTTP {
		proto = "=http,https"
	}
	args := []string{
		"-fsS", "-g", "--proto", proto, "--retry", "2", "--connect-timeout", "20", "--max-time", "900",
		"--max-filesize", strconv.FormatInt(max+1, 10), "-o", dest,
		"-w", "%{http_code}\n%{redirect_url}",
	}
	if resolveArg != "" {
		args = append(args, "--resolve", resolveArg)
	}
	if proxy != "" {
		args = append(args, "--proxy", proxy)
	} else {
		args = append(args, "--noproxy", "*")
	}
	args = append(args, "--", target)
	return args
}

func curlOnceOpts(ctx context.Context, target, dest string, max int64, resolveArg string, allowHTTP bool) (int, string, error) {
	curl, err := exec.LookPath("curl")
	if err != nil {
		return 0, "", err
	}
	args := buildCurlArgs(target, dest, max, resolveArg, allowHTTP, updateProxyURL)
	// -w 走 stdout,curl 的错误信息走 stderr,不能混在一起截。
	out, err := exec.CommandContext(ctx, curl, args...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return 0, "", fmt.Errorf("curl: %v %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return 0, "", fmt.Errorf("curl: %v", err)
	}
	// Windows 版 curl 的 -w 输出是 CRLF,两个字段都要各自 TrimSpace,
	// 否则 Atoi("302\r") 得 0,更新下载会被判成「返回 0」而全线失败。
	code, next, _ := strings.Cut(string(out), "\n")
	n, _ := strconv.Atoi(strings.TrimSpace(code))
	return n, strings.TrimSpace(next), nil
}

// absoluteURL 把 Location(可能是相对路径)解析成绝对地址。
func absoluteURL(base, ref string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", errors.New("更新地址非法")
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", errors.New("重定向地址非法")
	}
	return b.ResolveReference(r).String(), nil
}

// fetchUpdateJSON 优先用 curl(可走代理),失败再回落到 Go 更新探测客户端。
func fetchUpdateJSON(ctx context.Context, url string) ([]byte, error) {
	if err := validUpdateURL(url); err != nil {
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
		} else if errors.Is(err, netutil.ErrForbiddenAddress) {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.New("更新地址非法")
	}
	req.Header.Set("User-Agent", "fanke-updater/1.0")
	resp, err := updateManifestHTTPClient.Do(req)
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
