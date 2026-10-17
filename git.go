package main

import (
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
)

// emptyTree is git's well-known empty tree hash, used as diff base in repos without commits.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

type FileChange struct {
	Path    string
	OldPath string // set for renames and copies
	Status  string
	Added   int
	Deleted int
	Binary  bool
	Diff    string
}

type Repo struct {
	Root string
}

func OpenRepo(dir string) (*Repo, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return nil, fmt.Errorf("%s não é um repositório git", dir)
	}
	return &Repo{Root: strings.TrimSpace(string(out))}, nil
}

// git runs a git command that never takes optional locks, so polling does not
// rewrite .git/index and retrigger the file watcher.
func (r *Repo) git(args ...string) ([]byte, error) {
	base := []string{"--no-optional-locks", "-C", r.Root, "-c", "core.quotepath=off"}
	return exec.Command("git", append(base, args...)...).Output()
}

func (r *Repo) Branch() string {
	if out, err := r.git("symbolic-ref", "--short", "HEAD"); err == nil {
		return strings.TrimSpace(string(out))
	}
	if out, err := r.git("rev-parse", "--short", "HEAD"); err == nil {
		return strings.TrimSpace(string(out))
	}
	return "?"
}

// Commit identifies the commit the working tree is compared against.
type Commit struct {
	Hash, Short, Subject, Author, Date string
}

// HeadCommit returns HEAD, or nil in a repository without commits.
func (r *Repo) HeadCommit() *Commit {
	out, err := r.git("log", "-1", "--format=%H%x1f%h%x1f%s%x1f%an%x1f%cI", "HEAD")
	if err != nil {
		return nil
	}
	f := strings.Split(strings.TrimRight(string(out), "\n"), "\x1f")
	if len(f) != 5 {
		return nil
	}
	return &Commit{Hash: f[0], Short: f[1], Subject: f[2], Author: f[3], Date: f[4]}
}

func (r *Repo) base() string {
	if _, err := r.git("rev-parse", "--verify", "-q", "HEAD"); err == nil {
		return "HEAD"
	}
	return emptyTree
}

// Changes returns every modified, staged or untracked file with its diff against HEAD.
func (r *Repo) Changes() ([]FileChange, error) {
	out, err := r.git("status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	var files []FileChange
	parts := strings.Split(string(out), "\x00")
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		if len(p) < 4 {
			continue
		}
		xy := p[:2]
		f := FileChange{Path: p[3:], Status: statusLabel(xy)}
		if (xy[0] == 'R' || xy[0] == 'C') && i+1 < len(parts) {
			i++
			f.OldPath = parts[i]
		}
		files = append(files, f)
	}
	sort.Slice(files, func(a, b int) bool { return files[a].Path < files[b].Path })

	base := r.base()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range files {
		wg.Add(1)
		go func(f *FileChange) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			f.Diff = r.diff(f, base)
			f.Added, f.Deleted, f.Binary = countDiff(f.Diff)
		}(&files[i])
	}
	wg.Wait()
	return files, nil
}

func (r *Repo) diff(f *FileChange, base string) string {
	var out []byte
	var err error
	if f.Status == "?" {
		out, err = r.git("diff", "--no-color", "--no-index", "--", "/dev/null", f.Path)
	} else if f.OldPath != "" {
		out, err = r.git("diff", "--no-color", "-M", base, "--", f.OldPath, f.Path)
	} else {
		out, err = r.git("diff", "--no-color", base, "--", f.Path)
	}
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 1) {
		return ""
	}
	return string(out)
}

func statusLabel(xy string) string {
	if xy == "??" {
		return "?"
	}
	for _, c := range []byte{xy[0], xy[1]} {
		if c != ' ' && c != '.' {
			return string(c)
		}
	}
	return "M"
}

func countDiff(d string) (add, del int, binary bool) {
	for _, l := range strings.Split(d, "\n") {
		switch {
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
		case strings.HasPrefix(l, "+"):
			add++
		case strings.HasPrefix(l, "-"):
			del++
		case strings.HasPrefix(l, "Binary files"):
			binary = true
		}
	}
	return
}
