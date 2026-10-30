package web

import "embed"

// Files is bundled at build time; the deployed runtime does not need Node.js.
//
//go:embed all:dist
var Files embed.FS
