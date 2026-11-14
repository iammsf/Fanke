package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"

	"fanke/backend/internal/media"
	"fanke/backend/internal/repo"
)

const (
	AddSettingsItems       = "addSettingsItems"
	AddUploadMediaMenuItem = "addUploadMediaMenuItem"
	// AddEditorPanel 声明后,插件会出现在文章编辑器的「扩展工具」里,
	// 宿主以 iframe 打开插件 index.html?panel=1,内容同步走 postMessage。
	AddEditorPanel = "addEditorPanel"
	// EditorRegister 声明后,后台编辑器会加载插件的 editor.js,
	// 插件通过 window.FankeEditor.registerNode() 接管自定义标签的渲染与属性面板。
	EditorRegister = "editor.register"
)

var declarativeEvents = map[string]bool{
	AddSettingsItems:       true,
	AddUploadMediaMenuItem: true,
	AddEditorPanel:         true,
	EditorRegister:         true,
}

type MediaUploadContext struct {
	Filename  string
	SHA256    string
	Size      int64
	Extension string
	Storage   string
	Source    io.ReadSeeker
	Data      []byte
}
type MediaArtifact struct {
	File          string
	Width, Height int
}
type MediaProcessor func(context.Context, *App, *MediaUploadContext) ([]MediaArtifact, error)
type MediaProvider func(context.Context, *App) (media.Storage, media.S3Config, error)
type mediaPlugin struct {
	slug      string
	processor MediaProcessor
	provider  MediaProvider
	settings  SettingContribution
	menu      UploadMenuContribution
}
type SettingContribution struct {
	ID         string `json:"id"`
	PluginSlug string `json:"plugin_slug"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
	Order      int    `json:"-"`
}
type UploadMenuContribution struct {
	ID         string `json:"id"`
	IsDefault  bool   `json:"is_default,omitempty"`
	PluginSlug string `json:"plugin_slug"`
	Label      string `json:"label"`
	Storage    string `json:"storage"`
	Order      int    `json:"-"`
	Enabled    bool   `json:"-"`
}
type MediaCapabilities struct {
	SettingsItems   []SettingContribution    `json:"settings"`
	UploadMenuItems []UploadMenuContribution `json:"upload_menu"`
	ImageProcessing bool                     `json:"image_processing"`
	S3Storage       bool                     `json:"s3_storage"`
}

// Compiled registrations only; plugin packages never execute server-side JavaScript.
var mediaPlugins = map[string]mediaPlugin{
	"image-processing": {slug: "image-processing", processor: processImageUpload, settings: SettingContribution{"image-processing", "image-processing", "图片处理", "image-processing", 10}},
	"s3-storage": {slug: "s3-storage", provider: func(ctx context.Context, a *App) (media.Storage, media.S3Config, error) {
		store, cfg, err := a.s3Store(ctx)
		if err == nil && (!cfg.Enabled || store == nil) {
			err = fmt.Errorf("S3 storage is not configured or enabled")
		}
		return store, cfg, err
	}, settings: SettingContribution{"s3-storage", "s3-storage", "S3 对象存储", "s3-storage", 20}, menu: UploadMenuContribution{ID: "s3-storage", PluginSlug: "s3-storage", Storage: "s3", Order: 20}},
}

func declares(e repo.Extension, event string) bool {
	if !e.Enabled || !e.Initialized {
		return false
	}
	for _, h := range e.Hooks {
		if h == event {
			return true
		}
	}
	return false
}
func activeMediaPlugins(exts []repo.Extension) []mediaPlugin {
	out := []mediaPlugin{}
	for _, e := range exts {
		if p, ok := mediaPlugins[e.Slug]; ok && e.Enabled && e.Initialized {
			if !declares(e, BeforeMediaUpload) {
				p.processor = nil
				p.provider = nil
			}
			if !declares(e, AddSettingsItems) {
				p.settings = SettingContribution{}
			}
			if !declares(e, AddUploadMediaMenuItem) {
				p.menu = UploadMenuContribution{}
			}
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].slug < out[j].slug })
	return out
}
func (a *App) activeMediaPlugins(ctx context.Context) ([]mediaPlugin, error) {
	if a.DB == nil {
		return nil, nil
	}
	exts, err := a.DB.ListExtensions(ctx)
	if err != nil {
		return nil, err
	}
	return activeMediaPlugins(exts), nil
}
func (a *App) mediaPluginActive(ctx context.Context, slug string) bool {
	ps, err := a.activeMediaPlugins(ctx)
	if err != nil {
		return false
	}
	for _, p := range ps {
		if p.slug == slug && (p.processor != nil || p.provider != nil) {
			return true
		}
	}
	return false
}
func capabilitiesFor(exts []repo.Extension, cfg media.S3Config) MediaCapabilities {
	if cfg.ID == "" {
		out := capabilitiesForBuckets(exts, []media.S3Config{{ID: "legacy", Alias: cfg.Bucket, Bucket: cfg.Bucket, Enabled: cfg.Enabled, Endpoint: cfg.Endpoint, AccessKey: cfg.AccessKey, SecretKey: cfg.SecretKey}})
		for i := range out.UploadMenuItems {
			out.UploadMenuItems[i].ID = "s3-storage"
			out.UploadMenuItems[i].Storage = "s3"
			out.UploadMenuItems[i].IsDefault = false
		}
		return out
	}
	return capabilitiesForBuckets(exts, []media.S3Config{cfg})
}

func capabilitiesForBuckets(exts []repo.Extension, buckets []media.S3Config, defaultIDs ...string) MediaCapabilities {
	defaultID := ""
	if len(defaultIDs) > 0 {
		defaultID = defaultIDs[0]
	}
	if defaultID == "" && len(buckets) > 0 {
		defaultID = buckets[0].ID
	}
	out := MediaCapabilities{SettingsItems: []SettingContribution{}, UploadMenuItems: []UploadMenuContribution{}}
	for _, p := range activeMediaPlugins(exts) {
		if p.processor != nil {
			out.ImageProcessing = true
		}
		if p.provider != nil {
			out.S3Storage = true
		}
		if p.settings.ID != "" {
			out.SettingsItems = append(out.SettingsItems, p.settings)
		}
		if p.menu.ID != "" && p.provider != nil {
			for _, cfg := range buckets {
				if cfg.ID == "" {
					continue
				}
				m := p.menu
				m.ID = p.menu.ID + ":" + cfg.ID
				m.Label = "上传至" + cfg.Alias
				m.Storage = "s3:" + cfg.ID
				m.IsDefault = cfg.ID == defaultID
				m.Enabled = cfg.Enabled && cfg.Endpoint != "" && cfg.AccessKey != "" && cfg.SecretKey != "" && cfg.Bucket != ""
				out.UploadMenuItems = append(out.UploadMenuItems, m)
			}
		}
	}
	sort.Slice(out.SettingsItems, func(i, j int) bool { return out.SettingsItems[i].Order < out.SettingsItems[j].Order })
	return out
}
func (a *App) MediaCapabilities(w http.ResponseWriter, r *http.Request) {
	exts, err := a.DB.ListExtensions(r.Context())
	if err != nil {
		a.failInternal(w, r, "media capabilities", err)
		return
	}
	settings, err := a.DB.S3Settings(r.Context())
	if err != nil {
		a.failInternal(w, r, "media config", err)
		return
	}
	writeJSON(w, http.StatusOK, capabilitiesForBuckets(exts, settings.Buckets, settings.DefaultBucketID))
}
func (a *App) requireS3Plugin(w http.ResponseWriter, r *http.Request) bool {
	if !a.mediaPluginActive(r.Context(), "s3-storage") {
		a.fail(w, http.StatusConflict, "S3 插件未安装、初始化或启用")
		return false
	}
	return true
}
func (a *App) prepareMediaArtifacts(ctx context.Context, u *MediaUploadContext) ([]MediaArtifact, string) {
	ps, err := a.activeMediaPlugins(ctx)
	if err != nil {
		return nil, "原图已保留，读取插件失败"
	}
	var out []MediaArtifact
	for _, p := range ps {
		if p.processor != nil {
			artifacts, err := p.processor(ctx, a, u)
			out = append(out, artifacts...)
			if err != nil {
				return out, "原图已保留，图片处理失败: " + err.Error()
			}
		}
	}
	return out, ""
}
