package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"fanke/backend/internal/render"
	"fanke/backend/internal/repo"
)

// settingTagFallbacks 是核心自己的「标签回落账本」:安装/更新插件时按 info.json 的
// fallback 字段(标签 → HTML 模板)登记。卸载插件不删除账本——正文里已经写进去的
// 标签必须永远能渲染;插件禁用/卸载/文件损坏时,渲染端按账本自动回落。
const settingTagFallbacks = "tag_fallbacks"

type tagFallback struct {
	Slug    string `json:"slug"`
	Version string `json:"version"`
	HTML    string `json:"html"`
}

var fallbackTagRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)

// 占位符 {{属性名}} / {{caption}}:渲染端代入真实值并转义。
var fallbackPlaceholderRe = regexp.MustCompile(`\{\{\s*([a-zA-Z][a-zA-Z0-9_-]*)\s*\}\}`)

// 消毒器把 src/href 当 URL 校验,{{…}} 会被百分号编码毁掉;先把占位符换成合法哨兵 URL,
// 消毒完再换回来。哨兵路径只含字母数字与下划线,不会触发任何改写。
const fallbackSentinel = "https://fanke-fallback.invalid/"

// sanitizeFallbackTemplate 消毒插件声明的回落模板:script/事件属性/javascript: 在安装时就被剔掉,
// 渲染端只做占位符代入(值一律转义),不再有任何执行面。
func sanitizeFallbackTemplate(tpl string) (string, error) {
	if strings.TrimSpace(tpl) == "" {
		return "", errors.New("模板为空")
	}
	if len(tpl) > 2048 {
		return "", errors.New("模板过长(≤2KB)")
	}
	names := fallbackPlaceholderRe.FindAllStringSubmatch(tpl, -1)
	pre := fallbackPlaceholderRe.ReplaceAllStringFunc(tpl, func(m string) string {
		return fallbackSentinel + fallbackPlaceholderRe.FindStringSubmatch(m)[1]
	})
	clean := render.SanitizeHTML(pre)
	for _, n := range names {
		clean = strings.ReplaceAll(clean, fallbackSentinel+n[1], "{{"+n[1]+"}}")
	}
	return clean, nil
}

func loadTagFallbacks(ctx context.Context, db *repo.DB) (map[string]tagFallback, error) {
	out := map[string]tagFallback{}
	raw, err := db.GetSetting(ctx, settingTagFallbacks)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// mergeTagFallbacks 把一个插件新版本的声明并进账本(纯函数):同一插件的旧登记先清掉
// (新版本不再声明的标签随之移除);标签所有权唯一,已被别的插件登记则报错。
func mergeTagFallbacks(m map[string]tagFallback, slug, version string, fallback map[string]string) (map[string]tagFallback, error) {
	out := make(map[string]tagFallback, len(m)+len(fallback))
	for tag, rec := range m {
		if rec.Slug != slug {
			out[tag] = rec
		}
	}
	for tag, tpl := range fallback {
		if !fallbackTagRe.MatchString(tag) {
			return nil, errors.New("fallback 标签非法(仅小写字母数字与连字符): " + tag)
		}
		if rec, ok := out[tag]; ok && rec.Slug != slug {
			return nil, errors.New("标签 " + tag + " 已由插件 " + rec.Slug + " 登记,所有权唯一")
		}
		clean, err := sanitizeFallbackTemplate(tpl)
		if err != nil {
			return nil, errors.New("fallback[" + tag + "]: " + err.Error())
		}
		out[tag] = tagFallback{Slug: slug, Version: version, HTML: clean}
	}
	return out, nil
}

// validateTagFallbacks 在动文件系统之前先校验声明:标签名合法、模板能消毒、所有权不冲突。
// 只报错不落库;真正登记走 registerTagFallbacks,免得解压到一半才发现声明非法。
func validateTagFallbacks(ctx context.Context, db *repo.DB, slug string, fallback map[string]string) error {
	if len(fallback) == 0 {
		return nil
	}
	m, err := loadTagFallbacks(ctx, db)
	if err != nil {
		return err
	}
	_, err = mergeTagFallbacks(m, slug, "", fallback)
	return err
}

// registerTagFallbacks 把一个插件版本的声明登记进账本。即使 fallback 为空也要走一遍:
// 新版本不再声明的标签要从账本里清掉。读-改-写全在 settings 行锁内,
// 并发安装两个不同插件时不会各自读到旧账本再互相覆盖。
func registerTagFallbacks(ctx context.Context, db *repo.DB, slug, version string, fallback map[string]string) error {
	return db.MutateSettingLocked(ctx, settingTagFallbacks, func(raw json.RawMessage) (any, error) {
		m := map[string]tagFallback{}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &m); err != nil {
				return nil, err
			}
		}
		return mergeTagFallbacks(m, slug, version, fallback)
	})
}

// extensionHealthy 判定插件文件是否完好:详情页 index.html 必须在;声明了编辑器能力的
// 必须有 editor.js 或 editor.ts。文件损坏或被手动删掉时视为不在线,渲染端自动走回落,
// 后台插件页也能据此提示,而不是让编辑器加载一个 404 的脚本。
func (a *App) extensionHealthy(slug string, hooks []string) bool {
	dir := a.extDir(slug)
	fi, err := os.Stat(filepath.Join(dir, "index.html"))
	if err != nil || fi.IsDir() {
		return false
	}
	for _, h := range hooks {
		if h != EditorRegister {
			continue
		}
		for _, name := range []string{"editor.js", "editor.ts"} {
			if fi, err := os.Stat(filepath.Join(dir, name)); err == nil && !fi.IsDir() {
				return true
			}
		}
		return false
	}
	return true
}
