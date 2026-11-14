package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"fanke/backend/internal/ai"
	"fanke/backend/internal/repo"
)

// ai.chat 通用 AI 对话能力。插件自带提示词与结果解析,后端只负责按分组/模型挑选连接并转发。
//
// 参数:{ messages:[{role,content}], group?, model?, temperature?, max_tokens? }
// 返回:{ content, model, usage }
// group 为空时套用后台保存的「当前使用」选择;两者都为空则全部启用连接按权重负载均衡。
func hookAIChat(ctx context.Context, a *App, ext *repo.Extension, payload json.RawMessage) (any, error) {
	var req ai.ChatRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errors.New("参数格式错误")
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("messages 不能为空")
	}
	if len(req.Messages) > 50 {
		return nil, errors.New("messages 最多 50 条")
	}
	total := 0
	for i, m := range req.Messages {
		m.Role = strings.TrimSpace(m.Role)
		switch m.Role {
		case "system", "user", "assistant":
		default:
			return nil, fmt.Errorf("messages[%d] role 非法", i)
		}
		if strings.TrimSpace(m.Content) == "" {
			return nil, errors.New("messages 内容不能为空")
		}
		total += utf8.RuneCountInString(m.Content)
		req.Messages[i].Role = m.Role
	}
	if total > 200000 {
		return nil, errors.New("messages 内容过长")
	}
	if req.Temperature < 0 || req.Temperature > 2 {
		return nil, errors.New("temperature 须在 0-2 之间")
	}
	if req.MaxTokens < 0 || req.MaxTokens > 32000 {
		return nil, errors.New("max_tokens 须在 0-32000 之间")
	}
	if a.AI == nil {
		return nil, errors.New("AI 服务不可用")
	}
	a.applyAISelection(ctx, &req)
	resp, err := a.AI.Chat(ctx, req)
	if errors.Is(err, ai.ErrNoAIConnection) {
		return nil, errors.New("没有匹配的 AI 连接,请检查分组/模型与启用状态")
	}
	if err != nil {
		return nil, err
	}
	return resp, nil
}
