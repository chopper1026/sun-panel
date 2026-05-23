package assets

import (
	"embed"
	"strings"
)

//go:embed conf.example.ini lang/* version
var assetFS embed.FS

func Asset(name string) ([]byte, error) {
	name = strings.TrimPrefix(name, "assets/")
	return assetFS.ReadFile(name)
}
