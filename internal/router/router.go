package router

import (
	"html/template"
	"net/http"

	"networkdisk/internal/config"
	"networkdisk/internal/handler"
	"networkdisk/internal/logging"
	"networkdisk/internal/middleware"
)

type StopFunc func()

func New(authH *handler.AuthHandler, fileH *handler.FileHandler, sysH *handler.SystemHandler, shareH *handler.ShareHandler, searchH *handler.SearchHandler, tagH *handler.TagHandler, cfg *config.Config) (http.Handler, StopFunc) {
	authMw := middleware.Auth(cfg)
	csrfMw := middleware.CSRF()
	rateLimitMw, stopRateLimiter := middleware.RateLimit(cfg.Server.RateLimit, cfg.RateLimitWindowDuration(), cfg.Server.TrustedProxy)
	loggerMw := middleware.Logger()
	recoverMw := middleware.Recover()

	mux := http.NewServeMux()

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	mux.Handle("POST /api/auth/register", wrap(authH.Register, rateLimitMw, csrfMw))
	mux.Handle("POST /api/auth/login", wrap(authH.Login, rateLimitMw, csrfMw))
	mux.Handle("POST /api/auth/logout", wrap(authH.Logout, authMw, csrfMw))
	mux.Handle("POST /api/auth/refresh", wrap(authH.Refresh, csrfMw))
	mux.Handle("GET /api/auth/me", wrap(authH.Me, authMw))
	mux.Handle("PATCH /api/auth/password", wrap(authH.ChangePassword, authMw, csrfMw))
	mux.Handle("POST /api/auth/totp/enable", wrap(authH.EnableTOTP, authMw, csrfMw))
	mux.Handle("POST /api/auth/totp/verify", wrap(authH.VerifyTOTP, authMw, csrfMw))
	mux.Handle("POST /api/auth/totp/disable", wrap(authH.DisableTOTP, authMw, csrfMw))

	mux.Handle("GET /api/files", wrap(fileH.List, authMw))
	mux.Handle("POST /api/files/upload", wrap(fileH.Upload, authMw, csrfMw))
	mux.Handle("GET /api/files/download/{id}", wrap(fileH.Download, authMw))
	mux.Handle("GET /api/files/thumbnail/{id}", wrap(fileH.Thumbnail, authMw))
	mux.Handle("POST /api/files/mkdir", wrap(fileH.Mkdir, authMw, csrfMw))
	mux.Handle("PATCH /api/files/{id}", wrap(fileH.Rename, authMw, csrfMw))
	mux.Handle("DELETE /api/files/{id}", wrap(fileH.Delete, authMw, csrfMw))

	mux.Handle("GET /api/files/recycle", wrap(fileH.RecycleList, authMw))
	mux.Handle("POST /api/files/{id}/restore", wrap(fileH.Restore, authMw, csrfMw))
	mux.Handle("DELETE /api/files/{id}/permanent", wrap(fileH.PermanentDelete, authMw, csrfMw))
	mux.Handle("GET /api/files/permanent-preview/{id}", wrap(fileH.PermanentDeletePreview, authMw))
	mux.Handle("POST /api/files/{id}/copy", wrap(fileH.Copy, authMw, csrfMw))
	mux.Handle("POST /api/files/{id}/star", wrap(fileH.ToggleStar, authMw, csrfMw))
	mux.Handle("GET /api/files/starred", wrap(fileH.StarredList, authMw))
	mux.Handle("POST /api/files/batch-delete", wrap(fileH.BatchDelete, authMw, csrfMw))
	mux.Handle("POST /api/files/batch-move", wrap(fileH.BatchMove, authMw, csrfMw))
	mux.Handle("GET /api/files/download-zip", wrap(fileH.DownloadZip, authMw))
	mux.Handle("GET /api/files/recent", wrap(fileH.Recent, authMw))
	mux.Handle("GET /api/files/dashboard", wrap(fileH.Dashboard, authMw))

	mux.Handle("POST /api/files/upload/init", wrap(fileH.InitUpload, authMw, csrfMw))
	mux.Handle("POST /api/files/upload/chunk", wrap(fileH.UploadChunk, authMw, csrfMw))
	mux.Handle("POST /api/files/upload/complete", wrap(fileH.CompleteUpload, authMw, csrfMw))
	mux.Handle("GET /api/files/upload/status/{uploadId}", wrap(fileH.UploadStatus, authMw))

	mux.Handle("GET /api/files/preview/{id}", wrap(fileH.Preview, authMw))
	mux.Handle("POST /api/files/{id}/temp-link", wrap(fileH.TempLink, authMw, csrfMw))

	mux.Handle("GET /api/search", wrap(searchH.Search, authMw))
	mux.Handle("GET /api/search/suggest", wrap(searchH.Suggest, authMw))

	mux.Handle("GET /api/tags", wrap(tagH.List, authMw))
	mux.Handle("POST /api/tags", wrap(tagH.Create, authMw, csrfMw))
	mux.Handle("DELETE /api/tags/{id}", wrap(tagH.Delete, authMw, csrfMw))
	mux.Handle("GET /api/tags/{id}/files", wrap(tagH.FilesByTag, authMw))
	mux.Handle("POST /api/files/tags", wrap(tagH.BatchAttach, authMw, csrfMw))

	mux.Handle("POST /api/shares", wrap(shareH.Create, authMw, csrfMw))
	mux.Handle("GET /api/shares", wrap(shareH.List, authMw))
	mux.Handle("DELETE /api/shares/{id}", wrap(shareH.Delete, authMw, csrfMw))

	mux.Handle("GET /api/stats", wrap(sysH.StorageStats, authMw))
	mux.Handle("GET /api/audit-logs", wrap(sysH.AuditLogs, authMw))

	recycleTmpl := template.Must(template.ParseFiles("web/templates/base.html", "web/templates/recycle.html"))
	mux.Handle("GET /recycle", wrap(func(w http.ResponseWriter, r *http.Request) {
		recycleTmpl.ExecuteTemplate(w,"recycle.html", nil)
	}, csrfMw))

	loginTmpl := template.Must(template.ParseFiles("web/templates/base.html", "web/templates/login.html"))
	registerTmpl := template.Must(template.ParseFiles("web/templates/base.html", "web/templates/register.html"))
	indexTmpl := template.Must(template.ParseFiles("web/templates/base.html", "web/templates/index.html"))

	mux.Handle("GET /{$}", wrap(func(w http.ResponseWriter, r *http.Request) {
		indexTmpl.ExecuteTemplate(w,"index.html", nil)
	}, csrfMw))
	mux.Handle("GET /login", wrap(func(w http.ResponseWriter, r *http.Request) {
		loginTmpl.ExecuteTemplate(w,"login.html", nil)
	}, csrfMw))
	mux.Handle("GET /register", wrap(func(w http.ResponseWriter, r *http.Request) {
		registerTmpl.ExecuteTemplate(w,"register.html", nil)
	}, csrfMw))

	sharesTmpl := template.Must(template.ParseFiles("web/templates/base.html", "web/templates/shares.html"))
	mux.Handle("GET /shares", wrap(func(w http.ResponseWriter, r *http.Request) {
		sharesTmpl.ExecuteTemplate(w,"shares.html", nil)
	}, csrfMw))

	dashboardTmpl := template.Must(template.ParseFiles("web/templates/base.html", "web/templates/dashboard.html"))
	mux.Handle("GET /dashboard", wrap(func(w http.ResponseWriter, r *http.Request) {
		dashboardTmpl.ExecuteTemplate(w,"dashboard.html", nil)
	}, csrfMw))

	searchTmpl := template.Must(template.ParseFiles("web/templates/base.html", "web/templates/search.html"))
	mux.Handle("GET /search", wrap(func(w http.ResponseWriter, r *http.Request) {
		searchTmpl.ExecuteTemplate(w,"search.html", nil)
	}, csrfMw))

	mux.Handle("GET /s/{token}", wrap(shareH.ServePublicPage))
	mux.Handle("POST /s/{token}/verify", wrap(shareH.VerifyPassword, rateLimitMw))
	mux.Handle("GET /s/{token}/download", wrap(shareH.Download, rateLimitMw))

	mux.Handle("GET /d/{token}", wrap(shareH.TempDownload))

	corsMw := middleware.CORS(cfg.Server.AllowedOrigins)
	traceMw := middleware.TraceID()

	var h http.Handler = mux
	h = corsMw(h)
	h = recoverMw(h)
	h = loggerMw(h)
	h = traceMw(h)
	return h, stopRateLimiter
}

type middlewareFunc func(http.Handler) http.Handler

func wrap(handler http.HandlerFunc, mws ...middlewareFunc) http.Handler {
	var h http.Handler = handler
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func execTmpl(w http.ResponseWriter, r *http.Request, tmpl *template.Template, name string) {
	if err := tmpl.ExecuteTemplate(w, name, nil); err != nil {
		logging.Error(r.Context(), "router", "template render failed", "error", err, "template", name)
	}
}
