package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"fanke/backend/internal/repo"
)

// Hook 是插件可主动调用的能力，不包含仅由业务分发的生命周期事件。
type Hook struct {
	Name        string
	Description string
	Public      bool
	Fn          HookFunc
}

type HookFunc func(ctx context.Context, a *App, ext *repo.Extension, payload json.RawMessage) (any, error)

var hookRegistry = map[string]*Hook{}

func RegisterHook(h *Hook) {
	if h.Name == "" || h.Fn == nil {
		panic("hook: name and Fn required")
	}
	if lifecycleEvents[h.Name] {
		panic("hook: lifecycle event is reserved: " + h.Name)
	}
	if _, dup := hookRegistry[h.Name]; dup {
		panic("hook: duplicate " + h.Name)
	}
	hookRegistry[h.Name] = h
}

func lookupHook(name string) *Hook { return hookRegistry[name] }

// ValidateHooks 同时识别能力声明和生命周期订阅。
func ValidateHooks(names []string) []string {
	missing := []string{}
	for _, n := range names {
		if lookupHook(n) == nil && !lifecycleEvents[n] && !declarativeEvents[n] {
			missing = append(missing, n)
		}
	}
	return missing
}

// HookNames 用于管理端声明展示，不代表可公开调用的能力列表。
func HookNames() []string {
	out := make([]string, 0, len(hookRegistry)+len(lifecycleEvents))
	for n := range hookRegistry {
		out = append(out, n)
	}
	for n := range lifecycleEvents {
		out = append(out, n)
	}
	for n := range declarativeEvents {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (a *App) InvokeHook(ctx context.Context, ext *repo.Extension, name string, payload json.RawMessage, isPublic bool) (any, error) {
	if lifecycleEvents[name] {
		return nil, fmt.Errorf("生命周期事件不可手动调用: %s", name)
	}
	h := lookupHook(name)
	if h == nil {
		return nil, fmt.Errorf("hook 不存在: %s", name)
	}
	if isPublic && !h.Public {
		return nil, fmt.Errorf("hook 不开放公开调用: %s", name)
	}
	declared := false
	for _, d := range ext.Hooks {
		if d == name {
			declared = true
			break
		}
	}
	if !declared {
		return nil, fmt.Errorf("插件未声明该 hook: %s", name)
	}
	return h.Fn(ctx, a, ext, payload)
}
