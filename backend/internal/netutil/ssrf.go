// Package netutil 提供统一的内网 SSRF 防护、私有地址白名单与安全拨号辅助。
package netutil

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrForbiddenAddress 表示目标指向未在白名单中放行的内网/回环/云元数据/保留地址。
var ErrForbiddenAddress = errors.New("目标地址不允许访问内网或保留地址")

// blockedIPv6Nets 包含不应由服务端访问的特殊 IPv6 网段。
var blockedIPv6Nets = func() []*net.IPNet {
	cidrs := []string{
		"::/128",
		"::1/128",
		"64:ff9b::/96",   // NAT64
		"64:ff9b:1::/48", // NAT64 本地使用
		"100::/64",       // 丢弃前缀
		"2001::/32",      // Teredo
		"2001:2::/48",    // 基准测试
		"2001:db8::/32",  // 文档保留
		"2002::/16",      // 6to4(可能内嵌私有 IPv4)
		"fc00::/7",       // 唯一本地地址(ULA)
		"fe80::/10",      // 链路本地
		"ff00::/8",       // 组播
	}
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}()

// IsForbiddenIP 判断目标 IP 是否属于回环、私网、链路本地(含云元数据 169.254.169.254)、
// 未指定、CGNAT、组播或保留地址。自动将 IPv4-mapped IPv6 归约为 IPv4 校验。
func IsForbiddenIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 0, v4[0] == 10, v4[0] == 127:
			return true
		case v4[0] == 169 && v4[1] == 254:
			return true
		case v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31:
			return true
		case v4[0] == 192 && v4[1] == 168:
			return true
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127: // 100.64/10 CGNAT
			return true
		case v4[0] == 192 && v4[1] == 0 && v4[2] <= 2: // 192.0.0/24、192.0.2/24
			return true
		case v4[0] == 198 && (v4[1] == 18 || v4[1] == 19): // 198.18/15
			return true
		case v4[0] == 198 && v4[1] == 51 && v4[2] == 100: // 198.51.100/24
			return true
		case v4[0] == 203 && v4[1] == 0 && v4[2] == 113: // 203.0.113/24
			return true
		case v4[0] >= 224: // 组播 + 保留 + 广播
			return true
		}
		return false
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsPrivate() {
		return true
	}
	for _, n := range blockedIPv6Nets {
		if n.Contains(ip) {
			return true
		}
	}
	// 仅放行全球单播 2000::/3,其余(含已废弃的 site-local)一律拒绝
	return ip[0]&0xe0 != 0x20
}

// IsLocalhostName 判断主机名是否为本机保留名(localhost 或 *.localhost)。
func IsLocalhostName(host string) bool {
	h := normalizeHost(host)
	return h == "localhost" || strings.HasSuffix(h, ".localhost")
}

func normalizeHost(host string) string {
	h := strings.TrimSpace(host)
	if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		h = h[1 : len(h)-1]
	}
	h = strings.TrimSuffix(h, ".")
	return strings.ToLower(h)
}

// Allowlist 解析逗号分隔的私有主机/IP/CIDR 或通配符 "*" 白名单。
type Allowlist struct {
	allowAll  bool
	hosts     []string
	wildcards []string // 存 ".example.local"
	ips       []net.IP
	cidrs     []*net.IPNet
}

// ParseAllowlist 解析环境变量白名单(如 "127.0.0.1,localhost,192.168.0.0/16,*.internal" 或 "*")。
func ParseAllowlist(spec string) *Allowlist {
	al := &Allowlist{}
	for _, raw := range strings.Split(spec, ",") {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			continue
		}
		if tok == "*" {
			al.allowAll = true
			return al
		}
		if strings.Contains(tok, "/") {
			if _, ipnet, err := net.ParseCIDR(tok); err == nil {
				al.cidrs = append(al.cidrs, ipnet)
				continue
			}
		}
		// 容错:若误填了 host:port(且不是裸 IPv6),剥离端口再匹配主机/IP
		if net.ParseIP(tok) == nil {
			if h, _, err := net.SplitHostPort(tok); err == nil && h != "" {
				tok = h
			}
		}
		norm := normalizeHost(tok)
		if norm == "" {
			continue
		}
		if ip := net.ParseIP(norm); ip != nil {
			al.ips = append(al.ips, ip)
			continue
		}
		if strings.HasPrefix(norm, "*.") && len(norm) > 2 {
			al.wildcards = append(al.wildcards, norm[1:]) // ".domain"
			continue
		}
		al.hosts = append(al.hosts, norm)
	}
	return al
}

// AllowsAll 返回是否配置了通配符 "*"。
func (a *Allowlist) AllowsAll() bool {
	return a != nil && a.allowAll
}

// MatchHost 判断主机名是否直接命中白名单(含精确 host、*.suffix 或字面量 IP/CIDR)。
func (a *Allowlist) MatchHost(host string) bool {
	if a == nil {
		return false
	}
	if a.allowAll {
		return true
	}
	h := normalizeHost(host)
	if h == "" {
		return false
	}
	if ip := net.ParseIP(h); ip != nil {
		return a.MatchIP(ip)
	}
	for _, item := range a.hosts {
		if h == item {
			return true
		}
	}
	for _, suf := range a.wildcards {
		if strings.HasSuffix(h, suf) && len(h) > len(suf) {
			return true
		}
	}
	return false
}

// MatchIP 判断解析出的 IP 是否命中白名单中的精确 IP 或 CIDR。
func (a *Allowlist) MatchIP(ip net.IP) bool {
	if a == nil || ip == nil {
		return false
	}
	if a.allowAll {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, allowed := range a.ips {
		if allowed.Equal(ip) {
			return true
		}
	}
	for _, n := range a.cidrs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// Allowed 判断(host, ip) 是否被白名单显式放行。
func (a *Allowlist) Allowed(host string, ip net.IP) bool {
	if a == nil {
		return false
	}
	return a.allowAll || a.MatchHost(host) || a.MatchIP(ip)
}

// SafeDialContext 返回带内网 SSRF 拦截与可选白名单放行的 DialContext 函数。
// 连接阶段先解析 DNS 并校验每个真实 IP,再直接按安全 IP 拨号,消除 DNS 重绑定风险。
func SafeDialContext(allowlist string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	allow := ParseAllowlist(allowlist)
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, errors.New("目标地址非法")
		}
		if ip := net.ParseIP(normalizeHost(host)); ip != nil {
			if IsForbiddenIP(ip) && !allow.Allowed(host, ip) {
				return nil, ErrForbiddenAddress
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("目标地址无法解析")
		}
		allowedIPs := make([]net.IP, 0, len(ips))
		for _, ip := range ips {
			if !IsForbiddenIP(ip.IP) || allow.Allowed(host, ip.IP) {
				allowedIPs = append(allowedIPs, ip.IP)
			}
		}
		if len(allowedIPs) == 0 {
			return nil, ErrForbiddenAddress
		}
		var lastErr error = ErrForbiddenAddress
		for _, ip := range allowedIPs {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
}

// ValidateURL 做字符串/字面量级别的 URL 与内网地址预检(不发起 DNS 查询)。
func ValidateURL(rawURL, allowlist string, requireHTTPS bool) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" || len(rawURL) > 2048 || strings.ContainsAny(rawURL, "\r\n\x00") {
		return nil, errors.New("URL 非法")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return nil, errors.New("URL 非法")
	}
	if requireHTTPS {
		if u.Scheme != "https" {
			return nil, errors.New("仅允许 https:// 地址")
		}
	} else {
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, errors.New("仅支持 http(s) 地址")
		}
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, errors.New("URL 端口非法")
		}
	}
	allow := ParseAllowlist(allowlist)
	if allow.AllowsAll() {
		return u, nil
	}
	host := u.Hostname()
	if ip := net.ParseIP(normalizeHost(host)); ip != nil {
		if IsForbiddenIP(ip) && !allow.Allowed(host, ip) {
			return nil, ErrForbiddenAddress
		}
		return u, nil
	}
	if IsLocalhostName(host) && !allow.MatchHost(host) &&
		!allow.MatchIP(net.ParseIP("127.0.0.1")) && !allow.MatchIP(net.ParseIP("::1")) {
		return nil, ErrForbiddenAddress
	}
	return u, nil
}

// ValidateEndpointURL 校验外部服务端点(如 S3 endpoint):同时检查字面量与 DNS 解析结果,
// 若任一解析 IP 属于内网/元数据且未命中白名单,立即拒绝。
func ValidateEndpointURL(ctx context.Context, rawURL, allowlist string, requireHTTPS bool) error {
	u, err := ValidateURL(rawURL, allowlist, requireHTTPS)
	if err != nil {
		return err
	}
	allow := ParseAllowlist(allowlist)
	host := u.Hostname()
	if allow.AllowsAll() || allow.MatchHost(host) {
		return nil
	}
	if net.ParseIP(normalizeHost(host)) != nil {
		return nil
	}
	if ctx == nil {
		return nil
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return errors.New("目标地址无法解析")
	}
	for _, ip := range ips {
		if IsForbiddenIP(ip.IP) && !allow.Allowed(host, ip.IP) {
			return ErrForbiddenAddress
		}
	}
	return nil
}

// ResolveSafeIP 解析更新/下载 URL 的域名,要求全部解析出的 IP 均非内网/保留地址,
// 返回 (host, port, safeIP) 供 curl --resolve 钉死连接 IP,防止 DNS 重绑定 SSRF。
func ResolveSafeIP(ctx context.Context, rawURL string, allowHTTP bool) (host, port, safeIP string, err error) {
	u, err := ValidateURL(rawURL, "", !allowHTTP)
	if err != nil {
		return "", "", "", err
	}
	host = u.Hostname()
	port = u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	if ip := net.ParseIP(normalizeHost(host)); ip != nil {
		if IsForbiddenIP(ip) {
			return "", "", "", ErrForbiddenAddress
		}
		return host, port, ip.String(), nil
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return "", "", "", errors.New("目标地址无法解析")
	}
	var picked net.IP
	for _, ip := range ips {
		if IsForbiddenIP(ip.IP) {
			return "", "", "", ErrForbiddenAddress
		}
		if picked == nil || (picked.To4() == nil && ip.IP.To4() != nil) {
			picked = ip.IP
		}
	}
	if picked == nil {
		return "", "", "", ErrForbiddenAddress
	}
	return host, port, picked.String(), nil
}

// FormatCurlResolve 构造 curl --resolve 所需的 host:port:ip 参数(IPv6 自动加方括号)。
func FormatCurlResolve(host, port, ip string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	if strings.Contains(ip, ":") && !strings.HasPrefix(ip, "[") {
		ip = "[" + ip + "]"
	}
	return host + ":" + port + ":" + ip
}
