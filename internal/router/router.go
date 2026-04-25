package router

import (
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"networkdisk/internal/config"
	"networkdisk/internal/handler"
	"networkdisk/internal/middleware"
)

func New(authH *handler.AuthHandler, cfg *config.Config, logger *slog.Logger) http.Handler {
	authMw := middleware.Auth(cfg)
	csrfMw := middleware.CSRF()
	rateLimitMw := middleware.RateLimit(10, time.Minute)
	loggerMw := middleware.Logger(logger)
	recoverMw := middleware.Recover(logger)

	mux := http.NewServeMux()

	mux.Handle("POST /api/auth/register", wrap(authH.Register, rateLimitMw, csrfMw))
	mux.Handle("POST /api/auth/login", wrap(authH.Login, rateLimitMw, csrfMw))
	mux.Handle("POST /api/auth/logout", wrap(authH.Logout, authMw, csrfMw))
	mux.Handle("POST /api/auth/refresh", wrap(authH.Refresh, csrfMw))
	mux.Handle("GET /api/auth/me", wrap(authH.Me, authMw))
	mux.Handle("PATCH /api/auth/password", wrap(authH.ChangePassword, authMw, csrfMw))

	templates := template.Must(template.ParseGlob("web/templates/*.html"))
	mux.Handle("GET /login", wrap(func(w http.ResponseWriter, r *http.Request) {
		templates.ExecuteTemplate(w, "login.html", nil)
	}, csrfMw))
	mux.Handle("GET /register", wrap(func(w http.ResponseWriter, r *http.Request) {
		templates.ExecuteTemplate(w, "register.html", nil)
	}, csrfMw))

	var h http.Handler = mux
	h = recoverMw(h)
	h = loggerMw(h)
	return h
}

type middlewareFunc func(http.Handler) http.Handler

func wrap(handler http.HandlerFunc, mws ...middlewareFunc) http.Handler {
	var h http.Handler = handler
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
