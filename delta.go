package main

import (
	"bytes"
	"os/exec"
	"strconv"
	"strings"
)

type deltaKey struct {
	diff  string
	width int
}

type deltaEntry struct {
	key deltaKey
	out []string
}

// deltaRenderer pipes diffs through git-delta, caching output per diff and width.
type deltaRenderer struct {
	cache map[string]deltaEntry
}

func newDeltaRenderer() *deltaRenderer {
	return &deltaRenderer{cache: map[string]deltaEntry{}}
}

func deltaAvailable() bool {
	_, err := exec.LookPath("delta")
	return err == nil
}

// render returns nil when delta fails, so the caller can use the native renderer.
func (d *deltaRenderer) render(path, diff string, width int) []string {
	key := deltaKey{diff, width}
	if c, ok := d.cache[path]; ok && c.key == key {
		return c.out
	}
	cmd := exec.Command("delta", "--paging=never", "--line-numbers", "--file-style=omit",
		"--hunk-header-style=omit", "--width="+strconv.Itoa(width-2))
	cmd.Stdin = strings.NewReader(diff)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		out = append(out, "  "+l)
	}
	d.cache[path] = deltaEntry{key, out}
	return out
}
