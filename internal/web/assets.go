package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*.css static/*.json static/*.png
var staticFiles embed.FS

func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	return http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
}
