// Package web holds the browser client, embedded into the server binary.
package web

import "embed"

// Files is the static site: index.html, app code, styles and vendored three.js.
//
//go:embed index.html style.css js vendor
var Files embed.FS
