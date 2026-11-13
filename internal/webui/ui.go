// SPDX-License-Identifier: AGPL-3.0-only
package webui

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"os"
)

//go:embed templates/*.html assets/*
var bundled embed.FS

func Templates(dev bool) (*template.Template, error) {
	if dev {
		return template.ParseGlob("internal/webui/templates/*.html")
	}
	return template.ParseFS(bundled, "templates/*.html")
}
func Assets(dev bool) http.Handler {
	var assets fs.FS
	if dev {
		assets = os.DirFS("internal/webui/assets")
	} else {
		assets, _ = fs.Sub(bundled, "assets")
	}
	return http.StripPrefix("/assets/", http.FileServer(http.FS(assets)))
}
