package middleware

import (
	"net/http"
	"os"
	"strings"
)

var (
	notFoundHTML    []byte
	serverErrorHTML []byte
)

func LoadErrorPages() {
	notFoundHTML, _ = os.ReadFile("web/templates/error_404.html")
	serverErrorHTML, _ = os.ReadFile("web/templates/error_500.html")
}

func NotFoundHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if prefersHTML(r) && len(notFoundHTML) > 0 {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			w.Write(notFoundHTML)
		} else {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"未找到"}`))
		}
	}
}

func ServeErrorPage(w http.ResponseWriter, r *http.Request) {
	if prefersHTML(r) && len(serverErrorHTML) > 0 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(serverErrorHTML)
	} else {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write([]byte(`{"error":"服务器内部错误"}`))
	}
}

func prefersHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}
