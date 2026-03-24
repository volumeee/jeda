package ui

import (
	"embed"
	"net/http"
)

//go:embed index.html
var WebFS embed.FS

// Handler directly serves the embedded static files
func Handler() http.Handler {
	return http.FileServer(http.FS(WebFS))
}
