// Package ai 实现 AI 连接管理、负载均衡及多提供商（OpenAI + Claude）统一调用。
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"fanke/backend/internal/repo"
)

type Client struct {
	db *repo.DB
	hc *http.Client
}

func NewClient(db *repo.DB) *Client {
	return &Client{db: db, hc: &http.Client{Timeout: 60 * time.Second}}
}

type ChatRequest struct {
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	// Group 非空时只在同名分组内选择;Model 非空时进一步锁定组内某个模型。
	// 两者都为空则沿用全局"当前选择"或全部启用连接。
	Group string `json:"group,omitempty"`
	Model string `json:"model,omitempty"`
}
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type ChatResponse struct {
	Content string `json:"content"`
	Model   string `json:"model"`
	Usage   Usage  `json:"usage"`
}
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

var ErrNoAIConnection = errors.New("no enabled AI connection")

func (c *Client) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	conns, err := c.db.ListAIConnections(ctx, true)
	if err != nil {
		return ChatResponse{}, err
	}
	group := strings.TrimSpace(req.Group)
	model := strings.TrimSpace(req.Model)
	if group != "" || model != "" {
		filtered := make([]repo.AIConnection, 0, len(conns))
		for _, conn := range conns {
			if group != "" && conn.GroupName != group {
				continue
			}
			if model != "" && conn.Model != model {
				continue
			}
			filtered = append(filtered, conn)
		}
		conns = filtered
	}
	if len(conns) == 0 {
		return ChatResponse{}, ErrNoAIConnection
	}
	// 负载均衡失败转移:先按权重随机排序,依次尝试,任一成功即返回。
	// 单个模型报错(限流/欠费/上游 5xx)不会再直接把整次调用判死。
	ordered := weightedShuffle(conns)
	errs := make([]string, 0, len(ordered))
	for _, conn := range ordered {
		resp, err := c.chatWith(ctx, conn, req)
		if err == nil {
			return resp, nil
		}
		errs = append(errs, fmt.Sprintf("%s/%s: %v", conn.GroupName, conn.Model, err))
		if ctx.Err() != nil { // 整体超时或调用方取消,不再继续尝试
			return ChatResponse{}, ctx.Err()
		}
	}
	return ChatResponse{}, fmt.Errorf("所有 AI 连接均失败: %s", strings.Join(errs, " | "))
}

// chatWith 按 provider 分发到具体实现。
func (c *Client) chatWith(ctx context.Context, conn repo.AIConnection, req ChatRequest) (ChatResponse, error) {
	switch conn.Provider {
	case "openai":
		return c.chatOpenAI(ctx, conn, req)
	case "claude":
		return c.chatClaude(ctx, conn, req)
	default:
		return ChatResponse{}, fmt.Errorf("unknown provider: %s", conn.Provider)
	}
}

// weightedShuffle 按权重不放回抽样,得到"权重越大越靠前"的尝试顺序。
func weightedShuffle(conns []repo.AIConnection) []repo.AIConnection {
	pool := append([]repo.AIConnection(nil), conns...)
	out := make([]repo.AIConnection, 0, len(pool))
	for len(pool) > 0 {
		pick := weightedRandom(pool)
		out = append(out, pick)
		for i := range pool {
			if pool[i].ID == pick.ID {
				pool = append(pool[:i], pool[i+1:]...)
				break
			}
		}
	}
	return out
}

func weightedRandom(conns []repo.AIConnection) repo.AIConnection {
	total := 0
	for _, c := range conns {
		if c.Weight > 0 {
			total += c.Weight
		}
	}
	if total <= 0 {
		return conns[0]
	}
	r := rand.Intn(total)
	for _, c := range conns {
		if c.Weight > 0 {
			r -= c.Weight
			if r < 0 {
				return c
			}
		}
	}
	return conns[0]
}

func normalizeBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("invalid API URL")
	}
	u.RawQuery, u.Fragment = "", ""
	u.Path = strings.TrimRight(u.Path, "/")
	if strings.HasSuffix(u.Path, "/v1") {
		u.Path = strings.TrimSuffix(u.Path, "/v1")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func endpoint(raw, path string) (string, error) {
	base, err := normalizeBaseURL(raw)
	if err != nil {
		return "", err
	}
	return base + path, nil
}
func request(ctx context.Context, hc *http.Client, method, target string, body []byte, headers map[string]string) (*http.Response, error) {
	r, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return hc.Do(r)
}
func responseError(provider string, resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("%s error %d: %s", provider, resp.StatusCode, strings.TrimSpace(string(b)))
}

func (c *Client) chatOpenAI(ctx context.Context, conn repo.AIConnection, req ChatRequest) (ChatResponse, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	body, err := json.Marshal(map[string]any{"model": conn.Model, "messages": req.Messages, "temperature": req.Temperature, "max_tokens": maxTokens})
	if err != nil {
		return ChatResponse{}, err
	}
	target, err := endpoint(conn.APIURL, "/v1/chat/completions")
	if err != nil {
		return ChatResponse{}, err
	}
	resp, err := request(ctx, c.hc, "POST", target, body, map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + conn.APIKey})
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ChatResponse{}, responseError("openai", resp)
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage Usage `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ChatResponse{}, err
	}
	if len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return ChatResponse{}, errors.New("openai response has no content")
	}
	return ChatResponse{Content: result.Choices[0].Message.Content, Model: conn.Model, Usage: result.Usage}, nil
}

func (c *Client) chatClaude(ctx context.Context, conn repo.AIConnection, req ChatRequest) (ChatResponse, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	var system string
	messages := make([]Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == "system" {
			system += m.Content
			continue
		}
		messages = append(messages, m)
	}
	payload := map[string]any{"model": conn.Model, "messages": messages, "temperature": req.Temperature, "max_tokens": maxTokens}
	if system != "" {
		payload["system"] = system
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ChatResponse{}, err
	}
	target, err := endpoint(conn.APIURL, "/v1/messages")
	if err != nil {
		return ChatResponse{}, err
	}
	resp, err := request(ctx, c.hc, "POST", target, body, map[string]string{"Content-Type": "application/json", "x-api-key": conn.APIKey, "anthropic-version": "2023-06-01"})
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ChatResponse{}, responseError("claude", resp)
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ChatResponse{}, err
	}
	var content strings.Builder
	for _, block := range result.Content {
		if block.Type == "text" {
			content.WriteString(block.Text)
		}
	}
	if content.Len() == 0 {
		return ChatResponse{}, errors.New("claude response has no content")
	}
	return ChatResponse{Content: content.String(), Model: conn.Model, Usage: Usage{PromptTokens: result.Usage.InputTokens, CompletionTokens: result.Usage.OutputTokens, TotalTokens: result.Usage.InputTokens + result.Usage.OutputTokens}}, nil
}

func (c *Client) ListModels(ctx context.Context, provider, apiURL, apiKey string) ([]string, error) {
	switch provider {
	case "openai":
		return c.listModelsOpenAI(ctx, apiURL, apiKey)
	case "claude":
		return c.listModelsClaude(ctx, apiURL, apiKey)
	default:
		return nil, fmt.Errorf("unknown provider: %s", provider)
	}
}

type modelPage struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
	HasMore  bool   `json:"has_more"`
	LastID   string `json:"last_id"`
	NextPage string `json:"next_page"`
}

func (c *Client) listModels(ctx context.Context, apiURL, apiKey, provider string) ([]string, error) {
	var out []string
	after := ""
	for pages := 0; pages < 100; pages++ {
		target, err := endpoint(apiURL, "/v1/models")
		if err != nil {
			return nil, err
		}
		if after != "" {
			target += "?after_id=" + url.QueryEscape(after)
		}
		headers := map[string]string{}
		if provider == "openai" {
			headers["Authorization"] = "Bearer " + apiKey
		} else {
			headers["x-api-key"] = apiKey
			headers["anthropic-version"] = "2023-06-01"
		}
		resp, err := request(ctx, c.hc, "GET", target, nil, headers)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			err = responseError(provider, resp)
			resp.Body.Close()
			return nil, err
		}
		var page modelPage
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		for _, m := range page.Data {
			if m.ID != "" {
				out = append(out, m.ID)
			}
		}
		if !page.HasMore {
			break
		}
		next := page.LastID
		if next == "" {
			next = page.NextPage
		}
		if next == "" || next == after {
			return nil, errors.New("models pagination missing cursor")
		}
		after = next
	}
	return out, nil
}
func (c *Client) listModelsOpenAI(ctx context.Context, apiURL, apiKey string) ([]string, error) {
	return c.listModels(ctx, apiURL, apiKey, "openai")
}
func (c *Client) listModelsClaude(ctx context.Context, apiURL, apiKey string) ([]string, error) {
	return c.listModels(ctx, apiURL, apiKey, "claude")
}
