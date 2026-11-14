package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"

	"fanke/backend/internal/render"
	"fanke/backend/internal/repo"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
)

const (
	BeforePostCreate   = "beforePostCreate"
	AfterPostCreated   = "afterPostCreated"
	BeforePostUpdate   = "beforePostUpdate"
	AfterPostUpdated   = "afterPostUpdated"
	BeforePostDelete   = "beforePostDelete"
	AfterPostDeleted   = "afterPostDeleted"
	BeforeMediaUpload  = "beforeMediaUpload"
	AfterMediaUploaded = "afterMediaUploaded"
	BeforeMediaDelete  = "beforeMediaDelete"
	AfterMediaDeleted  = "afterMediaDeleted"
)

var lifecycleEvents = map[string]bool{
	BeforePostCreate: true, AfterPostCreated: true,
	BeforePostUpdate: true, AfterPostUpdated: true,
	BeforePostDelete: true, AfterPostDeleted: true,
	BeforeMediaUpload: true, AfterMediaUploaded: true, BeforeMediaDelete: true, AfterMediaDeleted: true,
}

// LifecycleData 的业务对象均为每个处理器独立的快照。只有正文保存的 before
// 事件会采纳 ContentHTML 的修改；Post、OldPost、Media 等字段的修改均被丢弃。
// Reused 区分上传去重；Variants 是派生处理结束时（或删除前）的记录。
// S3 同步导入、存储转移和文章快捷发布/公开/置顶不产生这些事件。
type LifecycleData struct {
	PostID      int64
	Post        *repo.PostInput
	OldPost     *repo.Post
	ContentHTML string
	Media       *repo.Media
	Variants    []repo.MediaVariant
	Reused      bool `json:"reused"`
}

type LifecycleHandler func(context.Context, repo.Extension, *LifecycleData) error

var lifecycleRegistry = struct {
	sync.RWMutex
	handlers map[string]map[string]LifecycleHandler
}{handlers: make(map[string]map[string]LifecycleHandler)}

// RegisterLifecycleHandler 注册编译进后端的 Go 处理器，不加载或执行插件 JavaScript。
// info.json request 声明事件只代表订阅，仍须安装、启用并初始化才会分发。
func RegisterLifecycleHandler(slug, event string, fn LifecycleHandler) {
	if slug == "" || !lifecycleEvents[event] || fn == nil {
		panic("lifecycle: slug, known event and handler required")
	}
	lifecycleRegistry.Lock()
	defer lifecycleRegistry.Unlock()
	if lifecycleRegistry.handlers[slug] == nil {
		lifecycleRegistry.handlers[slug] = make(map[string]LifecycleHandler)
	}
	if lifecycleRegistry.handlers[slug][event] != nil {
		panic("lifecycle: duplicate " + slug + "/" + event)
	}
	lifecycleRegistry.handlers[slug][event] = fn
}

// snapshot 的类型限于本文件中的 JSON 可序列化业务数据，包含指针、切片和时间。
func lifecycleSnapshot[T any](value T) T {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return out
}

type lifecycleDispatcher struct {
	list     func(context.Context) ([]repo.Extension, error)
	handlers map[string]map[string]LifecycleHandler
}

func (d lifecycleDispatcher) dispatch(ctx context.Context, event string, data *LifecycleData) error {
	if !lifecycleEvents[event] {
		return fmt.Errorf("unknown lifecycle event: %s", event)
	}
	before := event == BeforeMediaUpload || event == BeforePostCreate || event == BeforePostUpdate || event == BeforePostDelete || event == BeforeMediaDelete
	exts, err := d.list(ctx)
	if err != nil {
		if before {
			return fmt.Errorf("lifecycle %s list extensions: %w", event, err)
		}
		log.Printf("lifecycle %s list extensions: %v", event, err)
		return nil
	}
	sort.SliceStable(exts, func(i, j int) bool { return exts[i].Slug < exts[j].Slug })
	for _, ext := range exts {
		if !ext.Enabled || !ext.Initialized {
			continue
		}
		declared := false
		for _, name := range ext.Hooks {
			if name == event {
				declared = true
				break
			}
		}
		fn := d.handlers[ext.Slug][event]
		if !declared || fn == nil {
			continue
		}
		copy := lifecycleSnapshot(*data)
		if err := fn(ctx, lifecycleSnapshot(ext), &copy); err != nil {
			if before {
				return fmt.Errorf("lifecycle %s plugin %s: %w", event, ext.Slug, err)
			}
			log.Printf("lifecycle %s plugin %s: %v", event, ext.Slug, err)
			continue
		}
		if event == BeforePostCreate || event == BeforePostUpdate {
			data.ContentHTML = copy.ContentHTML
		}
	}
	return nil
}

func (a *App) dispatchLifecycle(ctx context.Context, event string, data *LifecycleData) error {
	lifecycleRegistry.RLock()
	handlers := make(map[string]map[string]LifecycleHandler, len(lifecycleRegistry.handlers))
	for slug, events := range lifecycleRegistry.handlers {
		handlers[slug] = make(map[string]LifecycleHandler, len(events))
		for name, fn := range events {
			handlers[slug][name] = fn
		}
	}
	lifecycleRegistry.RUnlock()
	return (lifecycleDispatcher{list: a.DB.ListExtensions, handlers: handlers}).dispatch(ctx, event, data)
}

// 使用与 render.Markdown 相同的转换器，但将首次安全清洗推迟到 before 完成之后，
// 避免缺闭合 img 被首次清洗丢失插件语义。无论输入是否已清洗，最后均再次清洗。
var lifecycleMarkdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM, extension.Table, extension.Typographer, extension.Footnote),
	goldmark.WithRendererOptions(goldmarkhtml.WithUnsafe()),
)

func renderPostMarkdown(src string) (string, error) {
	var out bytes.Buffer
	if err := lifecycleMarkdown.Convert([]byte(src), &out); err != nil {
		return "", err
	}
	return out.String(), nil
}

func preparePostSave(ctx context.Context, event string, id int64, in *repo.PostInput, old *repo.Post, dispatch func(context.Context, string, *LifecycleData) error) error {
	data := &LifecycleData{PostID: id, Post: in, OldPost: old, ContentHTML: in.ContentHTML}
	if err := dispatch(ctx, event, data); err != nil {
		return err
	}
	in.ContentHTML = render.SanitizeHTML(data.ContentHTML)
	return nil
}

func init() {
	for _, event := range []string{BeforePostCreate, BeforePostUpdate} {
		RegisterLifecycleHandler("image-viewer", event, func(_ context.Context, _ repo.Extension, data *LifecycleData) error {
			data.ContentHTML = render.NormalizePluginImages(data.ContentHTML)
			return nil
		})
	}
}
