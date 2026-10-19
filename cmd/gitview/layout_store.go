package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	gexport "github.com/athos/gitview/internal/graph/export"
)

// layoutFile is where gitview serve keeps the card positions the user gave
// the Architecture view of a repository: GitView's cache directory
// (GITVIEW_CACHE_DIR overrides), never the repository itself.
func layoutFile(root string) (string, error) {
	base := os.Getenv("GITVIEW_CACHE_DIR")
	if base == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(cache, "gitview")
	}
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(base, hex.EncodeToString(sum[:6]), "layout.json"), nil
}

// savedLayout returns the layout saved by gitview serve for the repository
// at root, or nil when there is none.
func savedLayout(root string) (json.RawMessage, error) {
	path, err := layoutFile(root)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return gexport.ParseLayout(data)
}

// exportLayout picks the layout an HTML export embeds: the file given with
// --layout, none with --no-layout, otherwise the one saved by gitview serve.
func exportLayout(root, file string, none bool) (json.RawMessage, error) {
	switch {
	case none:
		return nil, nil
	case file != "":
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		return gexport.ParseLayout(data)
	}
	return savedLayout(root)
}
