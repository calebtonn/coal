// Package web embeds the built web UI.
package web

import "embed"

// Dist holds the Vite build under dist/. It holds only dist/.gitkeep until
// make web has run.
//
//go:embed all:dist
var Dist embed.FS
