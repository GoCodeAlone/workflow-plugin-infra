package internal

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed config.yaml
var configData []byte

//go:embed all:ui_dist
var embeddedUIFS embed.FS

// extractAssets writes the embedded ui_dist tree to the process working
// directory so that the static.fileserver path injected by ConfigFragment
// resolves at runtime.
//
// ASSUMPTION: the plugin process's working directory at extract time equals
// its working directory at runtime (i.e. the directory the engine uses as the
// plugin's install root). This mirrors the same assumption in
// workflow-plugin-authz-ui. If they diverge, the injected static.fileserver
// root will 404.
func extractAssets() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	uiDir := filepath.Join(dir, "ui_dist")
	if _, err := os.Stat(filepath.Join(uiDir, "index.html")); err == nil {
		return nil
	}

	return fs.WalkDir(embeddedUIFS, "ui_dist", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		target := filepath.Join(dir, path)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		data, err := embeddedUIFS.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(target, data, 0644)
	})
}
