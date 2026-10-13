package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// Assets returns the http.FileSystem for the compiled VueJS frontend
func Assets() http.FileSystem {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return http.FS(distFS)
	}
	return http.FS(sub)
}
