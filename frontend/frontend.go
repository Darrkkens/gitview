// Package frontend holds GitView's web UI: a Vite + React + TypeScript app
// styled with Tailwind CSS and daisyUI (src/), built by `npm run build` into
// the single self-contained file dist/index.html. The build is committed, so
// `go build` and `go install` never need Node; internal/webui adds the data
// and the Content-Security-Policy to it.
package frontend

import _ "embed"

// Index is the built page: every script and style inline, no data.
//
//go:embed dist/index.html
var Index []byte
