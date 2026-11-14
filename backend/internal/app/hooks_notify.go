package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"fanke/backend/internal/repo"
)

// notify.send 让宿主在指定范围显示通知。后端只做参数校验与透传,
// 实际渲染由宿主前端桥接(ElNotification / 区域内提示 / 弹出式)完成。
//
// 参数:
//
//	{ message, title?, level?: info|success|warning|error, scope?: global|inline|modal, duration?: 毫秒 }
//
// 返回:校验后(补全默认值)的同一份参数,宿主据此渲染。
func init() {
	RegisterHook(&Hook{Name: "notify.send", Description: "请求宿主显示通知(global/inline/modal)", Fn: hookNotifySend})
}

func hookNotifySend(_ context.Context, _ *App, _ *repo.Extension, payload json.RawMessage) (any, error) {
	var req struct {
		Message  string `json:"message"`
		Title    string `json:"title"`
		Level    string `json:"level"`
		Scope    string `json:"scope"`
		Duration int    `json:"duration"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errors.New("参数格式错误")
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		return nil, errors.New("message 不能为空")
	}
	if utf8.RuneCountInString(req.Message) > 2000 {
		return nil, errors.New("message 不能超过 2000 字")
	}
	req.Title = strings.TrimSpace(req.Title)
	if utf8.RuneCountInString(req.Title) > 120 {
		return nil, errors.New("title 不能超过 120 字")
	}
	level := strings.TrimSpace(req.Level)
	switch level {
	case "":
		level = "info"
	case "info", "success", "warning", "error":
	default:
		return nil, errors.New("level 只能是 info/success/warning/error")
	}
	scope := strings.TrimSpace(req.Scope)
	switch scope {
	case "":
		scope = "global"
	case "global", "inline", "modal":
	default:
		return nil, errors.New("scope 只能是 global/inline/modal")
	}
	duration := req.Duration
	if scope == "modal" {
		duration = 0
	} else {
		if duration <= 0 {
			duration = 3000
		}
		if duration < 1000 {
			duration = 1000
		}
		if duration > 15000 {
			duration = 15000
		}
	}
	return map[string]any{
		"message": req.Message, "title": req.Title, "level": level, "scope": scope, "duration": duration,
	}, nil
}
