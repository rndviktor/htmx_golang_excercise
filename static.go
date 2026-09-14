package htmxgolangexcercise

import "embed"

//go:embed templates
var Files embed.FS

//go:embed static
var StaticFiles embed.FS