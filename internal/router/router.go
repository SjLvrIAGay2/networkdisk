package router

import (
	"html/template"
	"net/http"

	"networkdisk/internal/config"
	"networkdisk/internal/handler"
	"networkdisk/internal/middleware"
)

type StopFunc func()

func New(authH *handler.AuthHandler, fileH *handler.FileHandler, cfg *config.Config) (http.Handler, StopFunc) {
	authMw := middleware.Auth(cfg)
	csrfMw := middleware.CSRF()
	rateLimitMw, stopRateLimiter := middleware.RateLimit(cfg.Server.RateLimit, cfg.RateLimitWindowDuration())
	loggerMw := middleware.Logger()
	recoverMw := middleware.Recover()

	mux := http.NewServeMux()

	mux.Handle("POST /api/auth/register", wrap(authH.Register, rateLimitMw, csrfMw))
	mux.Handle("POST /api/auth/login", wrap(authH.Login, rateLimitMw, csrfMw))
	mux.Handle("POST /api/auth/logout", wrap(authH.Logout, authMw, csrfMw))
	mux.Handle("POST /api/auth/refresh", wrap(authH.Refresh, csrfMw))
	mux.Handle("GET /api/auth/me", wrap(authH.Me, authMw))
	mux.Handle("PATCH /api/auth/password", wrap(authH.ChangePassword, authMw, csrfMw))

	mux.Handle("GET /api/files", wrap(fileH.List, authMw))
	mux.Handle("POST /api/files/upload", wrap(fileH.Upload, authMw, csrfMw))
	mux.Handle("GET /api/files/download/{id}", wrap(fileH.Download, authMw))
	mux.Handle("GET /api/files/thumbnail/{id}", wrap(fileH.Thumbnail, authMw))
	mux.Handle("POST /api/files/mkdir", wrap(fileH.Mkdir, authMw, csrfMw))
	mux.Handle("PATCH /api/files/{id}", wrap(fileH.Rename, authMw, csrfMw))
	mux.Handle("DELETE /api/files/{id}", wrap(fileH.Delete, authMw, csrfMw))

	loginTmpl := template.Must(template.ParseFiles("web/templates/base.html", "web/templates/login.html"))
	registerTmpl := template.Must(template.ParseFiles("web/templates/base.html", "web/templates/register.html"))
	indexTmpl := template.Must(template.ParseFiles("web/templates/base.html", "web/templates/index.html"))

	mux.Handle("GET /{$}", wrap(func(w http.ResponseWriter, r *http.Request) {
		indexTmpl.ExecuteTemplate(w, "index.html", nil)
	}, csrfMw))
	mux.Handle("GET /login", wrap(func(w http.ResponseWriter, r *http.Request) {
		loginTmpl.ExecuteTemplate(w, "login.html", nil)
	}, csrfMw))
	mux.Handle("GET /register", wrap(func(w http.ResponseWriter, r *http.Request) {
		registerTmpl.ExecuteTemplate(w, "register.html", nil)
	}, csrfMw))

	var h http.Handler = mux
	h = recoverMw(h)
	h = loggerMw(h)
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
