// Package web serves the built React app (web/dist, produced by `npm run build`
// in the Docker build stage) from the Go binary.
package web

import (
	"embed"
	"net/http"
)

//go:embed all:dist
var dist embed.FS

// Handler serves files from dist and falls back to index.html for any other
// GET, so client-side routes like /contacts/42 load the app.
func Handler() http.Handler {
	panic("not implemented")
}
