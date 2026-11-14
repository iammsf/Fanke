package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"fanke/backend/internal/repo"
)

// 通用插件 hooks。面向所有插件开放,插件在 info.json 的 request 里按名声明。
//
// store.*     —— 每插件隔离的 KV 数据表(表名由 slug 派生:ext_data_<slug>),
//
//	k 为 TEXT UNIQUE 主键,v 为任意 JSON。插件之间互不可见。
//
// media.list  —— 读媒体库(供扫描类功能使用)。
// http.check_url —— 服务端探测 url 可达性(带 SSRF 白名单)。
//
// 注意:store.get_batch 是公开 hook(前台页面免登录调用,要求插件已启用),
// 意味着启用插件的存储内容对访客可读——插件不应把敏感数据放进 store。

func init() {
	RegisterHook(&Hook{Name: "store.ensure_table", Description: "创建本插件的数据表(幂等)", Fn: hookStoreEnsureTable})
	RegisterHook(&Hook{Name: "store.save", Description: "保存/更新一条记录(upsert)", Fn: hookStoreSave})
	RegisterHook(&Hook{Name: "store.get", Description: "按 key 读取一条记录", Fn: hookStoreGet})
	RegisterHook(&Hook{Name: "store.get_batch", Description: "批量按 key 读取记录", Public: true, Fn: hookStoreGetBatch})
	RegisterHook(&Hook{Name: "store.list", Description: "列出本插件全部记录", Fn: hookStoreList})
	RegisterHook(&Hook{Name: "store.import", Description: "批量导入记录(可选不覆盖已有)", Fn: hookStoreImport})
	RegisterHook(&Hook{Name: "store.export", Description: "导出本插件全部记录", Fn: hookStoreExport})
	RegisterHook(&Hook{Name: "store.dedup", Description: "去除重复 key 记录(兜底)", Fn: hookStoreDedup})
	RegisterHook(&Hook{Name: "store.delete", Description: "按 key 列表删除记录", Fn: hookStoreDelete})
	RegisterHook(&Hook{Name: "store.stats", Description: "返回本插件记录总数", Fn: hookStoreStats})
	RegisterHook(&Hook{Name: "media.list", Description: "列出媒体库文件(可按 mime 前缀过滤)", Fn: hookMediaList})
	RegisterHook(&Hook{Name: "http.check_url", Description: "服务端探测 url 可达性", Fn: hookHTTPCheckURL})
	RegisterHook(&Hook{Name: "ai.chat", Description: "调用 AI 对话(可按分组/模型选择连接)", Fn: hookAIChat})
}

// storeTable 由插件 slug 派生表名;slug 字符集 [a-z0-9-] 在安装时已校验,拼 SQL 安全。
func storeTable(ext *repo.Extension) string {
	return "ext_data_" + strings.ReplaceAll(ext.Slug, "-", "_")
}

func validStoreKey(k string) error {
	if k == "" || len(k) > 2048 {
		return errors.New("key 非法(长度须 1-2048)")
	}
	return nil
}

type storeRow struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt time.Time       `json:"updated_at"`
}

func scanStoreRow(row interface{ Scan(...any) error }) (*storeRow, error) {
	var r storeRow
	if err := row.Scan(&r.Key, &r.Value, &r.UpdatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

func hookStoreEnsureTable(ctx context.Context, a *App, ext *repo.Extension, _ json.RawMessage) (any, error) {
	t := storeTable(ext)
	_, err := a.DB.Exec(ctx, `CREATE TABLE IF NOT EXISTS `+t+` (
		id BIGSERIAL PRIMARY KEY,
		k TEXT NOT NULL UNIQUE,
		v JSONB NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	if err != nil {
		return nil, err
	}
	_, err = a.DB.Exec(ctx, `CREATE INDEX IF NOT EXISTS `+t+`_k_idx ON `+t+` (k)`)
	return map[string]any{"ok": err == nil, "table": t}, err
}

func hookStoreSave(ctx context.Context, a *App, ext *repo.Extension, payload json.RawMessage) (any, error) {
	var req struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(payload, &req); err != nil || len(req.Value) == 0 {
		return nil, errors.New("payload 解析失败,期望 {key, value}")
	}
	if err := validStoreKey(req.Key); err != nil {
		return nil, err
	}
	_, err := a.DB.Exec(ctx, `INSERT INTO `+storeTable(ext)+`(k, v) VALUES($1,$2)
		ON CONFLICT (k) DO UPDATE SET v=$2, updated_at=now()`, req.Key, req.Value)
	return map[string]any{"ok": err == nil}, err
}

func hookStoreGet(ctx context.Context, a *App, ext *repo.Extension, payload json.RawMessage) (any, error) {
	var req struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(payload, &req); err != nil || req.Key == "" {
		return nil, errors.New("key required")
	}
	row, err := scanStoreRow(a.DB.QueryRow(ctx,
		`SELECT k, v, updated_at FROM `+storeTable(ext)+` WHERE k=$1`, req.Key))
	if err != nil {
		return map[string]any{"found": false}, nil
	}
	return map[string]any{"found": true, "key": row.Key, "value": row.Value, "updated_at": row.UpdatedAt}, nil
}

func hookStoreGetBatch(ctx context.Context, a *App, ext *repo.Extension, payload json.RawMessage) (any, error) {
	var req struct {
		Keys []string `json:"keys"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errors.New("payload 解析失败")
	}
	if len(req.Keys) == 0 {
		return map[string]any{"items": map[string]json.RawMessage{}}, nil
	}
	if len(req.Keys) > 500 {
		return nil, errors.New("一次最多 500 条")
	}
	rows, err := a.DB.Query(ctx, `SELECT k, v, updated_at FROM `+storeTable(ext)+` WHERE k = ANY($1)`, req.Keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := map[string]json.RawMessage{}
	for rows.Next() {
		r, err := scanStoreRow(rows)
		if err != nil {
			return nil, err
		}
		items[r.Key] = r.Value
	}
	return map[string]any{"items": items}, rows.Err()
}

func hookStoreList(ctx context.Context, a *App, ext *repo.Extension, _ json.RawMessage) (any, error) {
	rows, err := a.DB.Query(ctx, `SELECT k, v, updated_at FROM `+storeTable(ext)+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []storeRow{}
	for rows.Next() {
		r, err := scanStoreRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return map[string]any{"items": out, "total": len(out)}, rows.Err()
}

func hookStoreImport(ctx context.Context, a *App, ext *repo.Extension, payload json.RawMessage) (any, error) {
	var req struct {
		Items []struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		} `json:"items"`
		Overwrite *bool `json:"overwrite"` // 默认 true;false = 已有 key 不动(扫描初始化场景)
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errors.New("payload 解析失败,期望 {items:[{key,value}...], overwrite?}")
	}
	if len(req.Items) > 5000 {
		return nil, errors.New("一次最多导入 5000 条")
	}
	overwrite := req.Overwrite == nil || *req.Overwrite
	query := `INSERT INTO ` + storeTable(ext) + `(k, v) VALUES($1,$2) ON CONFLICT (k) DO UPDATE SET v=$2, updated_at=now()`
	if !overwrite {
		query = `INSERT INTO ` + storeTable(ext) + `(k, v) VALUES($1,$2) ON CONFLICT (k) DO NOTHING`
	}
	ok, skipped := 0, 0
	for _, it := range req.Items {
		if validStoreKey(it.Key) != nil || len(it.Value) == 0 {
			skipped++
			continue
		}
		tag, err := a.DB.Exec(ctx, query, it.Key, it.Value)
		if err != nil || (!overwrite && tag.RowsAffected() == 0) {
			skipped++
			continue
		}
		ok++
	}
	return map[string]any{"imported": ok, "skipped": skipped}, nil
}

func hookStoreExport(ctx context.Context, a *App, ext *repo.Extension, _ json.RawMessage) (any, error) {
	rows, err := a.DB.Query(ctx, `SELECT k, v, updated_at FROM `+storeTable(ext)+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []storeRow{}
	for rows.Next() {
		r, err := scanStoreRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return map[string]any{
		"format":   "fanke-store/export@1",
		"plugin":   ext.Slug,
		"count":    len(out),
		"items":    out,
		"exported": time.Now().UTC(),
	}, rows.Err()
}

func hookStoreDedup(ctx context.Context, a *App, ext *repo.Extension, _ json.RawMessage) (any, error) {
	t := storeTable(ext)
	tag, err := a.DB.Exec(ctx, `DELETE FROM `+t+` a USING `+t+` b WHERE a.k = b.k AND a.id > b.id`)
	if err != nil {
		return nil, err
	}
	var total int
	_ = a.DB.QueryRow(ctx, `SELECT count(*) FROM `+t).Scan(&total)
	return map[string]any{"removed": tag.RowsAffected(), "total": total}, nil
}

func hookStoreDelete(ctx context.Context, a *App, ext *repo.Extension, payload json.RawMessage) (any, error) {
	var req struct {
		Keys []string `json:"keys"`
	}
	if err := json.Unmarshal(payload, &req); err != nil || len(req.Keys) == 0 {
		return nil, errors.New("keys required")
	}
	if len(req.Keys) > 1000 {
		return nil, errors.New("一次最多删除 1000 条")
	}
	tag, err := a.DB.Exec(ctx, `DELETE FROM `+storeTable(ext)+` WHERE k = ANY($1)`, req.Keys)
	if err != nil {
		return nil, err
	}
	return map[string]any{"deleted": tag.RowsAffected()}, nil
}

func hookStoreStats(ctx context.Context, a *App, ext *repo.Extension, _ json.RawMessage) (any, error) {
	var total int
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM `+storeTable(ext)).Scan(&total); err != nil {
		return nil, err
	}
	return map[string]any{"total": total}, nil
}

// ---------- media.list ----------

func hookMediaList(ctx context.Context, a *App, _ *repo.Extension, payload json.RawMessage) (any, error) {
	var req struct {
		MimePrefix string `json:"mime_prefix"` // 如 "image/";空 = 全部
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, errors.New("payload 解析失败")
		}
	}
	if len(req.MimePrefix) > 64 || strings.ContainsAny(req.MimePrefix, "$;'\"\\") {
		return nil, errors.New("mime_prefix 非法")
	}
	query := `SELECT id, filename, storage, key, url, mime, size, width, height, sha256, created_at FROM media`
	args := []any{}
	if req.MimePrefix != "" {
		query += ` WHERE mime LIKE $1 || '%'`
		args = append(args, req.MimePrefix)
	}
	query += ` ORDER BY id LIMIT 5000`
	rows, err := a.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []repo.Media{}
	for rows.Next() {
		var m repo.Media
		if err := rows.Scan(&m.ID, &m.Filename, &m.Storage, &m.Key, &m.URL, &m.MIME, &m.Size,
			&m.Width, &m.Height, &m.SHA256, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return map[string]any{"items": out, "total": len(out)}, rows.Err()
}

// ---------- http.check_url ----------

// blockedProbeNets 明确禁止探测的 IPv6 特殊网段(IPv4 特殊段在 isForbiddenProbeIP 里逐字节判断)。
var blockedProbeNets = func() []*net.IPNet {
	cidrs := []string{
		"2001:db8::/32", // 文档示例地址
		"64:ff9b::/96",  // NAT64,可映射到内网 IPv4
		"2001::/32",     // Teredo 隧道
		"2002::/16",     // 6to4
	}
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}()

// isForbiddenProbeIP 判断目标 IP 是否属于不应由服务端探测的地址:
// 回环、私网、链路本地(含云元数据 169.254.169.254)、CGNAT、组播、保留段与 IPv6 特殊段。
func isForbiddenProbeIP(ip net.IP) bool {
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
		case v4[0] == 192 && v4[1] == 0 && v4[2] <= 2: // 192.0.0/24、192.0.2/24 保留/文档
			return true
		case v4[0] == 198 && (v4[1] == 18 || v4[1] == 19): // 198.18/15 基准测试
			return true
		case v4[0] == 198 && v4[1] == 51 && v4[2] == 100: // 198.51.100/24 文档
			return true
		case v4[0] == 203 && v4[1] == 0 && v4[2] == 113: // 203.0.113/24 文档
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
	for _, n := range blockedProbeNets {
		if n.Contains(ip) {
			return true
		}
	}
	// 仅放行全球单播 2000::/3,其余(含已废弃的 site-local)一律拒绝
	return ip[0]&0xe0 != 0x20
}

// safeProbeDial 在建立连接时校验解析出的真实 IP,防 DNS 重绑定绕过 URL 层检查。
func safeProbeDial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, errors.New("目标地址非法")
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("目标地址无法解析")
	}
	d := &net.Dialer{Timeout: 5 * time.Second}
	var lastErr error = errors.New("目标地址不允许探测")
	for _, ip := range ips {
		if isForbiddenProbeIP(ip.IP) {
			continue
		}
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// probeHTTPClient 服务端探测专用客户端:不走环境代理(避免绕过 IP 校验),
// 连接与每次重定向都重新校验目标,防止探测内网/回环/云元数据。
var probeHTTPClient = &http.Client{
	Timeout: 6 * time.Second,
	Transport: &http.Transport{
		Proxy:               nil,
		DialContext:         safeProbeDial,
		TLSHandshakeTimeout: 5 * time.Second,
		DisableKeepAlives:   true,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 4 {
			return errors.New("重定向次数过多")
		}
		return validProbeURL(req.URL.String())
	},
}

// validProbeURL 仅允许 http(s) 或站内相对路径,并直接拒绝字面量内网地址,防 SSRF。
func validProbeURL(u string) error {
	if u == "" || len(u) > 2048 {
		return errors.New("url 非法")
	}
	if strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "//") {
		return nil // 站内路径
	}
	p, err := url.Parse(u)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
		return errors.New("仅支持 http(s) 或站内路径")
	}
	if ip := net.ParseIP(p.Hostname()); ip != nil && isForbiddenProbeIP(ip) {
		return errors.New("不允许探测内网或保留地址")
	}
	if strings.EqualFold(p.Hostname(), "localhost") || strings.HasSuffix(strings.ToLower(p.Hostname()), ".localhost") {
		return errors.New("不允许探测本机地址")
	}
	return nil
}

// headProbe 探测 url:HEAD 失败后回退 GET(range 1 字节),5s 超时;状态码 <400 判可达。
func headProbe(ctx context.Context, raw string) bool {
	if strings.HasPrefix(raw, "/") {
		return true // 站内路径视为可达
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cli := probeHTTPClient
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		req, err := http.NewRequestWithContext(ctx, method, raw, nil)
		if err != nil {
			return false
		}
		req.Header.Set("User-Agent", "fanke-hook-http/1.0")
		if method == http.MethodGet {
			req.Header.Set("Range", "bytes=0-0")
		}
		resp, err := cli.Do(req)
		if err != nil {
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 256))
		resp.Body.Close()
		if resp.StatusCode < 400 {
			return true
		}
		if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
			continue
		}
		return false
	}
	return false
}

func hookHTTPCheckURL(ctx context.Context, a *App, _ *repo.Extension, payload json.RawMessage) (any, error) {
	var req struct {
		URLs []string `json:"urls"`
	}
	if err := json.Unmarshal(payload, &req); err != nil || len(req.URLs) == 0 {
		return nil, errors.New("urls required")
	}
	if len(req.URLs) > 100 {
		return nil, errors.New("一次最多探测 100 条")
	}
	type res struct {
		ok  bool
		url string
	}
	ch := make(chan res, len(req.URLs))
	sem := make(chan struct{}, 8) // 限并发,避免打爆外网
	for _, u := range req.URLs {
		u := u
		if validProbeURL(u) != nil {
			ch <- res{false, u}
			continue
		}
		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			ch <- res{headProbe(ctx, u), u}
		}()
	}
	out := map[string]bool{}
	for range req.URLs {
		r := <-ch
		out[r.url] = r.ok
	}
	return map[string]any{"reachable": out}, nil
}
