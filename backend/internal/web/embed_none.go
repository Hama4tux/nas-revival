//go:build !embedui

package web

import (
	"io/fs"
	"testing/fstest"
)

// Dist without the embedui tag serves a placeholder, so the backend builds and
// tests without a frontend build.
func Dist() fs.FS {
	return fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>NAS Revival</title><p>UI not built. Run scripts/webui/build.sh.</p>")}}
}
