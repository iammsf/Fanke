package app

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"fanke/backend/internal/ai"
	"fanke/backend/internal/netutil"
	"fanke/backend/internal/repo"
)

// 全局"当前使用"选择:分组 + 可选模型。空模型表示组内按权重负载均衡。
const aiSelectionSetting = "ai_selection"

// aiCallTimeout 一次 AI 请求的整体上限(含组内失败转移的重试时间)。
const aiCallTimeout = 120 * time.Second

// 空分组名单(还没有模型的、仅占位的分组),与库中实际分组取并集。
const aiGroupsSetting = "ai_groups"

type aiSelection struct {
	Group string `json:"group"`
	Model string `json:"model"`
}

func (a *App) ListAIConnections(w http.ResponseWriter, r *http.Request) {
	conns, err := a.DB.ListAIConnections(r.Context(), false)
	if err != nil {
		a.failInternal(w, r, "list_ai_connections", err)
		return
	}
	for i := range conns {
		conns[i] = maskAIConnection(conns[i])
	}
	writeJSON(w, 200, conns)
}

func (a *App) GetAIConnection(w http.ResponseWriter, r *http.Request) {
	id := routeInt(r, "id")
	conn, err := a.DB.GetAIConnection(r.Context(), id)
	if err != nil {
		a.failInternal(w, r, "get_ai_connection", err)
		return
	}
	writeJSON(w, 200, maskAIConnection(conn))
}

func (a *App) allowPrivateAIHosts() string {
	if a.Cfg == nil {
		return ""
	}
	return a.Cfg.AllowPrivateAIHosts
}

func (a *App) CreateAIConnection(w http.ResponseWriter, r *http.Request) {
	var req repo.CreateAIConnection
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, 400, "invalid json")
		return
	}
	req.GroupName = strings.TrimSpace(req.GroupName)
	req.Name = strings.TrimSpace(req.Name)
	req.Model = strings.TrimSpace(req.Model)
	if req.Name == "" {
		req.Name = req.Model
	}
	if err := validateAIConnection(req.Name, req.GroupName, req.Provider, req.APIURL, req.APIKey, req.Model, req.Weight, a.allowPrivateAIHosts()); err != nil {
		a.fail(w, 400, err.Error())
		return
	}
	conn, err := a.DB.CreateAIConnection(r.Context(), req)
	if err != nil {
		a.failInternal(w, r, "create_ai_connection", err)
		return
	}
	writeJSON(w, 201, maskAIConnection(conn))
}

// CreateAIConnections 一次添加同一分组下的多个模型(共享 provider/API 地址/Key)。
func (a *App) CreateAIConnections(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Group    string `json:"group"`
		Provider string `json:"provider"`
		APIURL   string `json:"api_url"`
		APIKey   string `json:"api_key"`
		Enabled  *bool  `json:"enabled"`
		Models   []struct {
			Model  string `json:"model"`
			Weight int    `json:"weight"`
		} `json:"models"`
	}
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, 400, "invalid json")
		return
	}
	group := strings.TrimSpace(req.Group)
	if err := validateAIName(group, "分组名称"); err != nil {
		a.fail(w, 400, err.Error())
		return
	}
	// 只给分组名 + 模型时,复用该分组已保存的 Provider / API 地址 / 密钥
	provider := strings.TrimSpace(req.Provider)
	apiURL := strings.TrimSpace(req.APIURL)
	apiKey := req.APIKey
	if provider == "" || apiURL == "" || strings.TrimSpace(apiKey) == "" {
		p, u, k, err := a.DB.GroupCredentials(r.Context(), group)
		if err != nil || strings.TrimSpace(k) == "" {
			a.fail(w, 400, "该分组还没有保存过密钥,请填写 API 地址和 API Key")
			return
		}
		if provider == "" {
			provider = p
		}
		if apiURL == "" {
			apiURL = u
		}
		if strings.TrimSpace(apiKey) == "" {
			apiKey = k
		}
	}
	if err := validateAIEndpoint(provider, apiURL, apiKey, a.allowPrivateAIHosts()); err != nil {
		a.fail(w, 400, err.Error())
		return
	}
	if len(req.Models) == 0 {
		a.fail(w, 400, "至少选择一个模型")
		return
	}
	if len(req.Models) > 200 {
		a.fail(w, 400, "一次最多添加 200 个模型")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	items := make([]repo.CreateAIConnection, 0, len(req.Models))
	seen := make(map[string]bool, len(req.Models))
	for _, m := range req.Models {
		model := strings.TrimSpace(m.Model)
		if model == "" || utf8.RuneCountInString(model) > 200 {
			a.fail(w, 400, "模型名称非法")
			return
		}
		if seen[model] {
			a.fail(w, 400, "模型重复: "+model)
			return
		}
		seen[model] = true
		weight := m.Weight
		if weight <= 0 {
			weight = 1
		}
		if weight > 1_000_000 {
			a.fail(w, 400, "权重过大")
			return
		}
		items = append(items, repo.CreateAIConnection{
			Name:      model,
			GroupName: group,
			Provider:  provider,
			APIURL:    apiURL,
			APIKey:    apiKey,
			Model:     model,
			Enabled:   enabled,
			Weight:    weight,
		})
	}
	conns, err := a.DB.CreateAIConnections(r.Context(), items)
	if err != nil {
		a.failInternal(w, r, "create_ai_connections", err)
		return
	}
	for i := range conns {
		conns[i] = maskAIConnection(conns[i])
	}
	writeJSON(w, 201, map[string]any{"items": conns, "count": len(conns)})
}

func (a *App) UpdateAIConnection(w http.ResponseWriter, r *http.Request) {
	id := routeInt(r, "id")
	var req repo.UpdateAIConnection
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, 400, "invalid json")
		return
	}
	if req.GroupName != nil {
		v := strings.TrimSpace(*req.GroupName)
		if err := validateAIName(v, "分组名称"); err != nil {
			a.fail(w, 400, err.Error())
			return
		}
		req.GroupName = &v
	}
	if req.Model != nil {
		v := strings.TrimSpace(*req.Model)
		if v == "" || utf8.RuneCountInString(v) > 200 {
			a.fail(w, 400, "模型名称非法")
			return
		}
		req.Model = &v
	}
	if err := validateAIUpdate(req, a.allowPrivateAIHosts()); err != nil {
		a.fail(w, 400, err.Error())
		return
	}
	if req.APIKey != nil && strings.TrimSpace(*req.APIKey) == "" {
		req.APIKey = nil
	}
	conn, err := a.DB.UpdateAIConnection(r.Context(), id, req)
	if err != nil {
		a.failInternal(w, r, "update_ai_connection", err)
		return
	}
	writeJSON(w, 200, maskAIConnection(conn))
}

func (a *App) DeleteAIConnection(w http.ResponseWriter, r *http.Request) {
	id := routeInt(r, "id")
	if err := a.DB.DeleteAIConnection(r.Context(), id); err != nil {
		a.failInternal(w, r, "delete_ai_connection", err)
		return
	}
	w.WriteHeader(204)
}

// RenameAIGroup 重命名整个分组。
func (a *App) RenameAIGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, 400, "invalid json")
		return
	}
	from, to := strings.TrimSpace(req.From), strings.TrimSpace(req.To)
	if err := validateAIName(from, "原分组名称"); err != nil {
		a.fail(w, 400, err.Error())
		return
	}
	if err := validateAIName(to, "新分组名称"); err != nil {
		a.fail(w, 400, err.Error())
		return
	}
	if from == to {
		writeJSON(w, 200, map[string]any{"updated": 0})
		return
	}
	n, err := a.DB.RenameAIGroup(r.Context(), from, to)
	if err != nil {
		a.failInternal(w, r, "rename_ai_group", err)
		return
	}
	// 空分组名单与"当前使用"同步改名
	if extra, err := a.aiExtraGroups(r.Context()); err == nil {
		out := make([]string, 0, len(extra))
		seen := map[string]bool{}
		for _, x := range extra {
			if x == from {
				x = to
			}
			if x == "" || seen[x] {
				continue
			}
			seen[x] = true
			out = append(out, x)
		}
		_ = a.saveAIExtraGroups(r.Context(), out)
	}
	a.renameAISelection(r.Context(), from, to)
	writeJSON(w, 200, map[string]any{"updated": n})
}

// CreateAIGroup 新建空分组(仅登记名称,模型可稍后批量添加)。
func (a *App) CreateAIGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, 400, "invalid json")
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := validateAIName(name, "分组名称"); err != nil {
		a.fail(w, 400, err.Error())
		return
	}
	list, err := a.aiGroupList(r.Context())
	if err != nil {
		a.failInternal(w, r, "create_ai_group", err)
		return
	}
	for _, n := range list {
		if n == name {
			a.fail(w, 409, "分组已存在")
			return
		}
	}
	extra, _ := a.aiExtraGroups(r.Context())
	extra = append(extra, name)
	if err := a.saveAIExtraGroups(r.Context(), extra); err != nil {
		a.failInternal(w, r, "create_ai_group", err)
		return
	}
	writeJSON(w, 201, map[string]string{"name": name})
}

// DeleteAIGroup 删除分组。action=delete 时组内模型一并删除;
// action=move 时把组内模型迁移到 target 分组,模型本身不丢。
func (a *App) DeleteAIGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Action string `json:"action"`
		Target string `json:"target"`
	}
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, 400, "invalid json")
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := validateAIName(name, "分组名称"); err != nil {
		a.fail(w, 400, err.Error())
		return
	}
	action := strings.TrimSpace(req.Action)
	if action == "" {
		action = "delete"
	}
	if action != "delete" && action != "move" {
		a.fail(w, 400, "action 只能是 delete 或 move")
		return
	}
	target := ""
	if action == "move" {
		target = strings.TrimSpace(req.Target)
		if err := validateAIName(target, "目标分组"); err != nil {
			a.fail(w, 400, "迁移时必须选择目标分组")
			return
		}
		if target == name {
			a.fail(w, 400, "目标分组不能与要删除的分组相同")
			return
		}
		list, err := a.aiGroupList(r.Context())
		if err != nil {
			a.failInternal(w, r, "delete_ai_group", err)
			return
		}
		if !containsString(list, target) {
			a.fail(w, 400, "目标分组不存在")
			return
		}
	}
	affected, err := a.DB.DeleteAIGroup(r.Context(), name, target)
	if err != nil {
		a.failInternal(w, r, "delete_ai_group", err)
		return
	}
	// 清掉空分组名单里的旧名称;迁移目标若原本只是空分组则保留
	if extra, err := a.aiExtraGroups(r.Context()); err == nil {
		out := make([]string, 0, len(extra))
		for _, x := range extra {
			if x != name {
				out = append(out, x)
			}
		}
		_ = a.saveAIExtraGroups(r.Context(), out)
	}
	a.fixAISelection(r.Context(), name, action, target)
	writeJSON(w, 200, map[string]any{"affected": affected, "action": action, "target": target})
}

// aiGroupList 库中实际分组 + 空分组名单,去重后返回。
func (a *App) aiGroupList(ctx context.Context) ([]string, error) {
	names, err := a.DB.AIGroupNames(ctx)
	if err != nil {
		return nil, err
	}
	extra, _ := a.aiExtraGroups(ctx)
	seen := make(map[string]bool, len(names)+len(extra))
	out := make([]string, 0, len(names)+len(extra))
	for _, n := range append(names, extra...) {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, nil
}

// aiExtraGroups 读取 settings 中登记的空分组名单;不存在时返回空。
func (a *App) aiExtraGroups(ctx context.Context) ([]string, error) {
	raw, err := a.DB.GetSetting(ctx, aiGroupsSetting)
	if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil, err
	}
	return names, nil
}

func (a *App) saveAIExtraGroups(ctx context.Context, names []string) error {
	return a.DB.PutSetting(ctx, aiGroupsSetting, names)
}

func (a *App) renameAISelection(ctx context.Context, from, to string) {
	raw, err := a.DB.GetSetting(ctx, aiSelectionSetting)
	if err != nil {
		return
	}
	var sel aiSelection
	if json.Unmarshal(raw, &sel) != nil {
		return
	}
	if strings.TrimSpace(sel.Group) != from {
		return
	}
	sel.Group = to
	_ = a.DB.PutSetting(ctx, aiSelectionSetting, sel)
}

// fixAISelection 分组被删除后修正"当前使用":迁移则改指目标分组,
// 连带删除时改指剩余分组(没有则清空,调用方会提示无可用连接)。
func (a *App) fixAISelection(ctx context.Context, name, action, target string) {
	raw, err := a.DB.GetSetting(ctx, aiSelectionSetting)
	if err != nil {
		return
	}
	var sel aiSelection
	if json.Unmarshal(raw, &sel) != nil {
		return
	}
	if strings.TrimSpace(sel.Group) != name {
		return
	}
	if action == "move" {
		sel.Group = target
	} else {
		sel.Group = ""
		sel.Model = ""
		if list, err := a.aiGroupList(ctx); err == nil && len(list) > 0 {
			sel.Group = list[0]
		}
	}
	_ = a.DB.PutSetting(ctx, aiSelectionSetting, sel)
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (a *App) ListAIModels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		APIURL   string `json:"api_url"`
		APIKey   string `json:"api_key"`
		Group    string `json:"group"`
	}
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, 400, "invalid json")
		return
	}
	provider := strings.TrimSpace(req.Provider)
	apiURL := strings.TrimSpace(req.APIURL)
	apiKey := req.APIKey
	// 只给分组名时复用该分组已保存的凭据(前端拿不到明文密钥)
	if apiURL == "" || strings.TrimSpace(apiKey) == "" {
		group := strings.TrimSpace(req.Group)
		if group == "" {
			a.fail(w, 400, "请填写 API 地址和 API Key")
			return
		}
		p, u, k, err := a.DB.GroupCredentials(r.Context(), group)
		if err != nil || strings.TrimSpace(k) == "" {
			a.fail(w, 400, "该分组还没有保存过密钥,请先填写 API 地址和 API Key")
			return
		}
		if provider == "" {
			provider = p
		}
		apiURL, apiKey = u, k
	}
	if err := validateAIEndpoint(provider, apiURL, apiKey, a.allowPrivateAIHosts()); err != nil {
		a.fail(w, 400, err.Error())
		return
	}
	models, err := a.AI.ListModels(r.Context(), provider, apiURL, apiKey)
	if err != nil {
		log.Printf("ai list models failed: %v", err)
		a.fail(w, 502, "failed to query models")
		return
	}
	writeJSON(w, 200, map[string]any{"models": models})
}

func (a *App) ChatWithAI(w http.ResponseWriter, r *http.Request) {
	var req ai.ChatRequest
	if err := decodeJSON(r, &req); err != nil {
		a.fail(w, 400, "invalid json")
		return
	}
	a.applyAISelection(r.Context(), &req)
	// 失败转移会串行重试组内所有模型,必须有整体上限,否则最坏 N×60s 才返回。
	ctx, cancel := context.WithTimeout(r.Context(), aiCallTimeout)
	defer cancel()
	resp, err := a.AI.Chat(ctx, req)
	if errors.Is(err, ai.ErrNoAIConnection) {
		a.fail(w, 409, "没有匹配的 AI 连接,请检查分组/模型与启用状态")
		return
	}
	if err != nil {
		log.Printf("ai chat failed: %v", err)
		a.fail(w, http.StatusBadGateway, "AI 请求失败，请检查模型配置或稍后重试")
		return
	}
	writeJSON(w, 200, resp)
}

// applyAISelection 调用方未显式指定分组/模型时,套用后台保存的"当前使用"选择。
func (a *App) applyAISelection(ctx context.Context, req *ai.ChatRequest) {
	if a.DB == nil {
		return
	}
	if strings.TrimSpace(req.Group) != "" || strings.TrimSpace(req.Model) != "" {
		return
	}
	raw, err := a.DB.GetSetting(ctx, aiSelectionSetting)
	if err != nil {
		return
	}
	var sel aiSelection
	if json.Unmarshal(raw, &sel) != nil {
		return
	}
	req.Group = strings.TrimSpace(sel.Group)
	req.Model = strings.TrimSpace(sel.Model)
}

// maskAIConnection 对外响应统一脱敏:明文密钥不出后端。
func maskAIConnection(c repo.AIConnection) repo.AIConnection {
	c.APIKey = maskAPIKey(c.APIKey)
	c.APIURL = maskAPIURL(c.APIURL)
	return c
}

// maskAPIKey 只保留末 4 位,其余遮住;过短的密钥整体遮住。
func maskAPIKey(key string) string {
	k := strings.TrimSpace(key)
	if k == "" {
		return ""
	}
	r := []rune(k)
	if len(r) <= 4 {
		return strings.Repeat("•", len(r))
	}
	return strings.Repeat("•", 8) + string(r[len(r)-4:])
}

// maskAPIURL 遮住 URL 里的 userinfo 与 key/token/secret/password 类查询参数。
func maskAPIURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return raw
	}
	if u.User != nil {
		u.User = url.UserPassword("••••", "••••")
	}
	q := u.Query()
	changed := false
	for k := range q {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "key") || strings.Contains(lk, "token") ||
			strings.Contains(lk, "secret") || strings.Contains(lk, "password") {
			q.Set(k, "••••")
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func validateAIName(name, label string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New(label + "不能为空")
	}
	if utf8.RuneCountInString(name) > 64 {
		return errors.New(label + "不能超过 64 个字符")
	}
	if strings.ContainsAny(name, "\r\n\t") {
		return errors.New(label + "不能包含换行或制表符")
	}
	return nil
}

func validateAIEndpoint(provider, rawURL, key, allowPrivateHosts string) error {
	if strings.TrimSpace(provider) == "" || strings.TrimSpace(rawURL) == "" || strings.TrimSpace(key) == "" {
		return errors.New("missing required fields")
	}
	if provider != "openai" && provider != "claude" {
		return errors.New("provider must be openai or claude")
	}
	if _, err := netutil.ValidateURL(rawURL, allowPrivateHosts, false); err != nil {
		if errors.Is(err, netutil.ErrForbiddenAddress) {
			return errors.New("禁止访问内网/本机 AI 地址(如需连接内网自建服务请在 ALLOW_PRIVATE_AI_HOSTS 中放行)")
		}
		return errors.New("invalid API URL")
	}
	return nil
}

func validateAIConnection(name, group, provider, rawURL, key, model string, weight int, allowPrivateHosts string) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(model) == "" {
		return errors.New("missing required fields")
	}
	if err := validateAIName(group, "分组名称"); err != nil {
		return err
	}
	if err := validateAIEndpoint(provider, rawURL, key, allowPrivateHosts); err != nil {
		return err
	}
	if weight <= 0 {
		return errors.New("weight must be greater than zero")
	}
	return nil
}

func validateAIUpdate(u repo.UpdateAIConnection, allowPrivateHosts string) error {
	if u.Name != nil && strings.TrimSpace(*u.Name) == "" || u.Model != nil && strings.TrimSpace(*u.Model) == "" || u.APIURL != nil && strings.TrimSpace(*u.APIURL) == "" {
		return errors.New("string fields must not be empty")
	}
	if u.Provider != nil && *u.Provider != "openai" && *u.Provider != "claude" {
		return errors.New("provider must be openai or claude")
	}
	if u.APIURL != nil {
		if _, err := netutil.ValidateURL(*u.APIURL, allowPrivateHosts, false); err != nil {
			if errors.Is(err, netutil.ErrForbiddenAddress) {
				return errors.New("禁止访问内网/本机 AI 地址(如需连接内网自建服务请在 ALLOW_PRIVATE_AI_HOSTS 中放行)")
			}
			return errors.New("invalid API URL")
		}
	}
	if u.Weight != nil && *u.Weight <= 0 {
		return errors.New("weight must be greater than zero")
	}
	return nil
}

func routeInt(r *http.Request, key string) int {
	id, _ := strconv.Atoi(chi.URLParam(r, key))
	return id
}
