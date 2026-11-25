//go:build embedui

package web

import (
	"embed"
	"io/fs"
)

// dist is copied from frontend/dist by scripts/webui/build.sh before building
// with -tags embedui.
//
//go:embed all:dist
var embedded embed.FS

func Dist() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
