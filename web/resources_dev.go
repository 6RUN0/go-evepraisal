//go:build dev
// +build dev

package web

import (
	"io/fs"
	"os"
	"path/filepath"
)

var osPath, _ = filepath.Abs("web/resources")

// Resources reads web/resources from disk at run time.
var Resources = os.DirFS(osPath)

// StaticFS holds the static files served under /static/.
var StaticFS, _ = fs.Sub(Resources, "static")

// TemplateFS holds the HTML templates.
var TemplateFS, _ = fs.Sub(Resources, "templates")
