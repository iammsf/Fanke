package app

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

func (a *App) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.Recoverer)
	// 不再使用 chimw.RealIP:它无条件信任 X-Forwarded-For,客户端可伪造来源 IP。
	// 真实 IP 由 OpenResty 写入 X-Real-IP,见 ipHash。
	r.Use(a.securityHeaders)
	r.Use(a.accessLog)
	r.Use(chimw.RequestID, chimw.Throttle(64))

	r.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
		status := a.setupState()
		code := http.StatusOK
		if a.DB != nil {
			ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
			defer cancel()
			if err := a.DB.Ping(ctx); err != nil {
				status = "db_unreachable"
				code = http.StatusServiceUnavailable
			}
		}
		writeJSON(w, code, map[string]string{"status": "ok", "db": status})
	})

	// 本地媒体卷直读
	r.Get("/uploads/*", a.ServeLocalMedia)

	// SEO:sitemap 由后端按库中内容实时生成(静态站无构建期路由清单)
	r.Get("/sitemap.xml", a.SitemapXML)

	// 按需缩图(WebP):/img?src=...&w=...,命中缓存直接返回,否则生成后长期强缓存
	r.Get("/img", a.ServeThumb)

	r.Route("/api/v1", func(r chi.Router) {
		// 版本号公开,便于确认部署的是哪个构建
		r.Get("/version", a.Version)
		// 安装/找回向导(引导模式下唯一可用接口)
		r.Get("/setup/status", a.SetupStatus)
		r.With(a.rateLimitLogin).Post("/setup/db", a.SetupDB)
		r.With(a.rateLimitLogin).Post("/setup", a.SetupComplete)

		// 以下业务接口要求数据库正常,否则 503(前端据此跳向导)
		r.Group(func(r chi.Router) {
			r.Use(a.requireDB)
			r.Use(noStore) // API 响应禁止 CDN/浏览器缓存

			// 认证
			r.With(a.rateLimitLogin).Post("/auth/login", a.Login)
			r.Post("/auth/refresh", a.Refresh)
			r.Post("/auth/logout", a.Logout)
			r.With(a.bearerAuth).Get("/auth/me", a.Me)
			r.With(a.bearerAuth).Post("/auth/password", a.ChangePassword)
			r.With(a.bearerAuth).Put("/auth/me", a.UpdateMyProfile)

			// 公开读(带可选 Bearer:已登录用户可预览未公开文章)
			r.Get("/posts", a.ListPostsPublic)
			r.With(a.optionalBearer).Get("/posts/{slug}", a.GetPost)
			r.With(a.optionalBearer).Get("/posts/{slug}/comments", a.ListComments)
			r.With(a.optionalBearer).Post("/posts/{slug}/comments", a.PostComment)
			r.Get("/terms", a.ListTerms)
			r.Get("/settings", a.PublicSettings)
			r.Get("/themes", a.ListThemes)
			r.Get("/themes/{name}/tokens.css", a.GetThemeCSS)
			r.Get("/plugins", a.ListPluginsPublic)

			// 插件(扩展):公开页面/资源/启用列表/公开 hook
			r.Get("/ext", a.ListEnabledExtensions)
			r.Get("/ext/{slug}/page", a.ServeExtensionPage)
			r.Get("/ext/{slug}/page/*", a.ServeExtensionPageFile)
			r.Get("/ext/{slug}/assets/*", a.ServeExtensionAsset)
			r.With(chimw.Throttle(60)).Post("/ext/{slug}/hook/{hook}", a.InvokeExtensionHookPublic)

			// 管理端
			r.Group(func(r chi.Router) {
				r.Use(a.requireRole("author"))
				r.Use(a.dedupeWrites)
				r.Get("/admin/posts", a.ListPostsAdmin)
				r.Get("/admin/posts/{id}", a.GetPostAdmin)
				r.Post("/admin/posts", a.CreatePost)
				r.Put("/admin/posts/{id}", a.UpdatePost)
				r.Delete("/admin/posts/{id}", a.DeletePost)
				r.Post("/admin/posts/{id}/publish", a.PublishPost)
				r.Post("/admin/posts/{id}/public", a.SetPostPublic)
				r.Post("/admin/posts/{id}/sticky", a.SetPostSticky)
				r.Get("/admin/posts-sticky", a.ListStickyPosts)
				r.Put("/admin/posts-sticky", a.SetStickyOrder)
				r.Post("/admin/media", a.UploadMedia)
				r.Get("/admin/media", a.ListMedia)
				r.Delete("/admin/media/{id}", a.DeleteMedia)
				r.Post("/admin/media/batch-delete", a.BatchDeleteMedia)
				r.Post("/admin/media/batch-transfer-s3", a.BatchTransferS3)
				r.Post("/admin/media/batch-download", a.BatchDownloadMedia)
				r.Post("/admin/media/sync/scan", a.SyncScan)
				r.Post("/admin/media/sync/apply", a.SyncApply)
				r.Get("/admin/terms", a.ListTerms)
				r.Post("/admin/terms", a.CreateTerm)
				r.Put("/admin/terms/{id}", a.UpdateTerm)
				r.Delete("/admin/terms/{id}", a.DeleteTerm)
				r.Get("/admin/extensions/contributions", a.MediaCapabilities)
				r.Get("/admin/extensions/editor-panels", a.ListEditorPanels)
			})
			r.Group(func(r chi.Router) {
				r.Use(a.requireRole("admin"))
				r.Use(a.dedupeWrites)
				// 博客本体版本检查与升级(升级只替换 bin/web,插件目录不受影响)
				r.Get("/admin/update/check", a.CheckCoreUpdate)
				r.Post("/admin/update/apply", a.ApplyCoreUpdate)
				// 站点设置含可执行 HTML(页脚)/导航/主题等,只能由管理员修改,避免作者越权注入脚本。
				r.Get("/admin/settings", a.AllSettings)
				r.Put("/admin/settings", a.PutSetting)
				r.Get("/admin/comments", a.ListCommentsAdmin)
				r.Put("/admin/comments/{id}/status", a.SetCommentStatus)
				r.Delete("/admin/comments/{id}", a.DeleteComment)
				r.Get("/admin/media/s3", a.GetS3Config)
				r.Get("/admin/media/image-processing", a.GetImageProcessing)
				r.Put("/admin/media/image-processing", a.PutImageProcessing)
				r.Put("/admin/media/s3", a.PutS3Config)
				r.Post("/admin/media/s3/buckets", a.S3Buckets)
				r.Post("/admin/media/s3/test", a.S3Test)
				r.Post("/admin/media/s3/cache-control", a.CacheControlBackfill)
				r.Post("/admin/themes", a.UploadTheme)
				r.Delete("/admin/themes/{name}", a.DeleteTheme)
				r.Get("/admin/plugins", a.ListPluginsAdmin)
				r.Post("/admin/plugins", a.CreatePlugin)
				r.Put("/admin/plugins/{id}", a.UpdatePlugin)
				r.Delete("/admin/plugins/{id}", a.DeletePlugin)
				// 插件(扩展)管理
				r.Get("/admin/extensions", a.ListExtensions)
				r.Post("/admin/extensions", a.UploadExtension)
				r.Get("/admin/extensions/{slug}", a.GetExtension)
				r.Post("/admin/extensions/{slug}/enabled", a.SetExtensionEnabled)
				r.Post("/admin/extensions/{slug}/initialized", a.MarkExtensionInitialized)
				r.Post("/admin/extensions/{slug}/check-update", a.CheckExtensionUpdate)
				r.Delete("/admin/extensions/{slug}", a.DeleteExtension)
				r.Post("/admin/extensions/{slug}/hook/{hook}", a.InvokeExtensionHookAdmin)
				// AI 连接管理
				r.Get("/admin/ai-connections", a.ListAIConnections)
				r.Post("/admin/ai-connections", a.CreateAIConnection)
				r.Post("/admin/ai-connections/batch", a.CreateAIConnections)
				r.Post("/admin/ai-connections/group-rename", a.RenameAIGroup)
				r.Post("/admin/ai-connections/group-create", a.CreateAIGroup)
				r.Post("/admin/ai-connections/group-delete", a.DeleteAIGroup)
				r.Get("/admin/ai-connections/{id}", a.GetAIConnection)
				r.Put("/admin/ai-connections/{id}", a.UpdateAIConnection)
				r.Delete("/admin/ai-connections/{id}", a.DeleteAIConnection)
				r.Post("/admin/ai-models", a.ListAIModels)
				r.Post("/admin/ai-chat", a.ChatWithAI)
				r.Get("/admin/users", a.ListUsersAdmin)
				r.Post("/admin/users", a.CreateUserAdmin)
				r.Put("/admin/users/{id}", a.UpdateUserAdmin)
				r.Delete("/admin/users/{id}", a.DeleteUserAdmin)
				r.Post("/admin/users/{id}/password", a.ResetUserPassword)
			})
		})
	})
	return r
}

func (a *App) rateLimitLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.allowLogin(r) {
			a.fail(w, http.StatusTooManyRequests, "too many attempts")
			return
		}
		next.ServeHTTP(w, r)
	})
}
