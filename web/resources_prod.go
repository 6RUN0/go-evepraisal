//go:build !dev
// +build !dev

package web

import (
	"embed"
	"io/fs"
)

//go:embed resources resources/templates/_*
var embedFS embed.FS

// Resources is the embedded web/resources tree.
var Resources, _ = fs.Sub(embedFS, "resources")

// StaticFS holds the static files served under /static/.
var StaticFS, _ = fs.Sub(Resources, "static")

// TemplateFS holds the HTML templates.
var TemplateFS, _ = fs.Sub(Resources, "templates")
