// Package web holds the compiled Svelte UI (web/dist), embedded into the binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built UI rooted at dist/.
func FS() fs.FS {
	f, _ := fs.Sub(dist, "dist")
	return f
}
