package static

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist/*
var distFS embed.FS

// GetFileSystem returns an http.FileSystem for the embedded dist directory.
func GetFileSystem() http.FileSystem {
	fsys, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return http.FS(fsys)
}

// HasDist returns true if the dist directory exists and has content.
func HasDist() bool {
	entries, err := fs.ReadDir(distFS, "dist")
	if err != nil {
		return false
	}
	return len(entries) > 0
}
