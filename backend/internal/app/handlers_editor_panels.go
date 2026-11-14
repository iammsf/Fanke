package app

import "net/http"

// EditorPanelContribution 声明 addEditorPanel 的插件在文章编辑器中得到的入口。
type EditorPanelContribution struct {
	PluginSlug  string `json:"plugin_slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ListEditorPanels 返回可挂进编辑器的插件面板(已初始化 + 已启用 + 声明 addEditorPanel)。
// 后端只做通用发现,面板内容与逻辑全部在插件包内。
func (a *App) ListEditorPanels(w http.ResponseWriter, r *http.Request) {
	if a.DB == nil {
		writeJSON(w, http.StatusOK, map[string]any{"panels": []EditorPanelContribution{}})
		return
	}
	exts, err := a.DB.ListExtensions(r.Context())
	if err != nil {
		a.failInternal(w, r, "editor panels", err)
		return
	}
	panels := []EditorPanelContribution{}
	for _, e := range exts {
		if !declares(e, AddEditorPanel) {
			continue
		}
		panels = append(panels, EditorPanelContribution{
			PluginSlug:  e.Slug,
			Name:        e.Name,
			Description: e.Description,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"panels": panels})
}
