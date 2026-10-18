package frontend

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Repository content reaches the page as data and must only ever be
// rendered as text: React escapes it, and no code may bypass that.
func TestSourcesUseNoHTMLSinks(t *testing.T) {
	sinks := regexp.MustCompile(`dangerouslySetInnerHTML|\.innerHTML\s*=|outerHTML\s*=|insertAdjacentHTML|document\.write|\beval\(|new Function\(`)
	err := filepath.WalkDir("src", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !(strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx")) {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range bytes.Split(data, []byte("\n")) {
			if sinks.Match(line) {
				t.Errorf("%s:%d uses an HTML/code sink: %s", path, i+1, strings.TrimSpace(string(line)))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The build is one file with everything inline.
func TestBuildIsSelfContained(t *testing.T) {
	page := string(Index)
	if !strings.Contains(page, "<title>GitView</title>") || !strings.Contains(page, `<script type="module"`) {
		t.Fatal("dist/index.html is not a GitView build (run `make web`)")
	}
	if regexp.MustCompile(`(?i)<script[^>]+\bsrc=|<link[^>]+rel="?stylesheet|@import\s+url`).MatchString(page) {
		t.Error("the build loads scripts or styles from elsewhere")
	}
	if regexp.MustCompile(`(?i)(src|href)="(https?:)?//`).MatchString(page) {
		t.Error("the build references a remote URL")
	}
}
