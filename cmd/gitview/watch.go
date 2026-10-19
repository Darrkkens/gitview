package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounce = 150 * time.Millisecond

var skipDirs = map[string]bool{".git": true, "node_modules": true, ".venv": true, "__pycache__": true}

// watch emits a (debounced) signal on the returned channel whenever the work
// tree or the git index/HEAD changes. Polling remains the fallback if inotify fails.
func watch(root string) (<-chan struct{}, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	addTree(w, root)
	_ = w.Add(filepath.Join(root, ".git"))

	out := make(chan struct{}, 1)
	notify := func() {
		select {
		case out <- struct{}{}:
		default:
		}
	}
	go func() {
		var t *time.Timer
		for {
			select {
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if strings.HasSuffix(ev.Name, ".lock") {
					continue
				}
				if ev.Op&fsnotify.Create != 0 {
					if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
						addTree(w, ev.Name)
					}
				}
				if t == nil {
					t = time.AfterFunc(debounce, notify)
				} else {
					t.Reset(debounce)
				}
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return out, nil
}

func addTree(w *fsnotify.Watcher, dir string) {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		_ = w.Add(p)
		return nil
	})
}
