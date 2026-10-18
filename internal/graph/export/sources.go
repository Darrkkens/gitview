package export

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/athos/gitview/internal/graph/model"
)

// Limits of the source bundle: a file larger than maxSourceFile is left out
// (the page says so), and the bundle stops growing at maxSourceTotal.
const (
	maxSourceFile  = 2 << 20
	maxSourceTotal = 48 << 20
)

// SourceBundle is the Go source the page shows in its Source tab.
type SourceBundle struct {
	Encoded string   // base64 of the gzip-compressed JSON object path → content
	Files   int      // files included
	Bytes   int      // uncompressed size of the included files
	Skipped []string // files left out, with the reason
}

// sources reads the Go files the graph has file nodes for, and only those:
// repository-relative .go paths that stay inside the root, regular files
// (no symlinks), valid UTF-8. The files are read, never executed or
// modified. The bundle is deterministic: sorted keys, gzip header without
// name or time.
func sources(g *model.Graph) (SourceBundle, error) {
	var b SourceBundle
	if g.Root == "" {
		return b, nil
	}
	var paths []string
	seen := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Type != model.KindFile || n.External || n.File == "" || seen[n.File] {
			continue
		}
		seen[n.File] = true
		paths = append(paths, n.File)
	}
	sort.Strings(paths)
	root, err := filepath.EvalSymlinks(g.Root)
	if err != nil {
		return b, nil
	}
	files := map[string]string{}
	for _, p := range paths {
		clean := filepath.Clean(filepath.FromSlash(p))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || !strings.HasSuffix(clean, ".go") {
			b.Skipped = append(b.Skipped, p+": not a repository Go file")
			continue
		}
		abs := filepath.Join(root, clean)
		// A directory on the way may be a symlink to somewhere else.
		if real, err := filepath.EvalSymlinks(filepath.Dir(abs)); err != nil || (real != root && !strings.HasPrefix(real, root+string(filepath.Separator))) {
			b.Skipped = append(b.Skipped, p+": outside the repository")
			continue
		}
		fi, err := os.Lstat(abs)
		switch {
		case err != nil:
			b.Skipped = append(b.Skipped, p+": "+err.Error())
			continue
		case !fi.Mode().IsRegular():
			b.Skipped = append(b.Skipped, p+": not a regular file")
			continue
		case fi.Size() > maxSourceFile:
			b.Skipped = append(b.Skipped, p+": larger than 2 MiB")
			continue
		case b.Bytes+int(fi.Size()) > maxSourceTotal:
			b.Skipped = append(b.Skipped, p+": source bundle limit reached")
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			b.Skipped = append(b.Skipped, p+": "+err.Error())
			continue
		}
		if !utf8.Valid(data) {
			b.Skipped = append(b.Skipped, p+": not valid UTF-8")
			continue
		}
		files[p] = string(data)
		b.Files++
		b.Bytes += len(data)
	}
	if len(files) == 0 {
		return b, nil
	}
	js, err := json.Marshal(files) // map keys are sorted
	if err != nil {
		return b, err
	}
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return b, err
	}
	if _, err := zw.Write(js); err != nil {
		return b, err
	}
	if err := zw.Close(); err != nil {
		return b, err
	}
	b.Encoded = base64.StdEncoding.EncodeToString(buf.Bytes())
	return b, nil
}
