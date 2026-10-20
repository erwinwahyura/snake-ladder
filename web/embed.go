// Package web holds the browser client, embedded into the server binary.
package web

import "embed"

// Files is the static site: index.html, app code, styles and vendored three.js.
//
//go:embed index.html offline.html sw.js style.css manifest.webmanifest js icons vendor
var Files embed.FS
