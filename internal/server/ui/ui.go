package ui

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist
var webFS embed.FS

func Wrap(api http.Handler) http.Handler {
	dist, err := fs.Sub(webFS, "dist")
	if err != nil {
		return api
	}
	fileServer := http.FileServer(http.FS(dist))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		f, err := dist.Open(path)
		if err != nil {
			f, err = dist.Open("index.html")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer f.Close()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.Copy(w, f)
			return
		}
		f.Close()
		fileServer.ServeHTTP(w, r)
	})
}
