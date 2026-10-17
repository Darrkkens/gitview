// Package run executes the Go toolchain commands the Deep Inspector needs
// (tests with coverage or traces, benchmarks, builds with compiler
// diagnostics) under strict rules: argument vectors only (no shell), a
// working directory inside the repository, a timeout, cancellation that
// reaches child processes, and bounded output.
//
// Nothing here decides to run anything: callers show Spec.CommandLine() to
// the user and run only after explicit consent.
package run

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxOutput = 16 << 20

type Spec struct {
	Args    []string
	Dir     string
	Env     []string // added to the environment
	Timeout time.Duration
	Live    io.Writer // optional: receives stdout and stderr as they are produced
}

type Result struct {
	ExitCode  int
	Stdout    []byte
	Stderr    []byte
	Duration  time.Duration
	Err       error // start failure, timeout or cancellation (not a non-zero exit)
	TimedOut  bool
	Cancelled bool
	Truncated bool
}

// CommandLine renders the command for display, quoting where needed.
func (s Spec) CommandLine() string {
	var parts []string
	for _, a := range s.Args {
		if a == "" || strings.ContainsAny(a, " \t\"'$`\\|&;<>()*?[]{}") {
			parts = append(parts, fmt.Sprintf("%q", a))
		} else {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}

type limited struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	cut  bool
	live io.Writer
}

func (l *limited) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.live != nil {
		l.live.Write(p)
	}
	if room := maxOutput - l.buf.Len(); room > 0 {
		if len(p) > room {
			l.buf.Write(p[:room])
			l.cut = true
		} else {
			l.buf.Write(p)
		}
	} else {
		l.cut = true
	}
	return len(p), nil
}

type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// scrubbedEnv is the environment without GitView's own secrets: the
// PostgreSQL connection given to EXPLAIN is not passed to project code.
func scrubbedEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GITVIEW_PG_DSN=") {
			out = append(out, kv)
		}
	}
	return out
}

// Run executes the command and waits for it.
func Run(ctx context.Context, s Spec) Result {
	if len(s.Args) == 0 {
		return Result{Err: errors.New("empty command"), ExitCode: -1}
	}
	if s.Timeout <= 0 {
		s.Timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.Args[0], s.Args[1:]...)
	cmd.Dir = s.Dir
	cmd.Env = append(scrubbedEnv(), s.Env...)
	cmd.WaitDelay = 3 * time.Second
	prepare(cmd)
	var out, errb limited
	if s.Live != nil {
		live := &syncWriter{w: s.Live}
		out.live, errb.live = live, live
	}
	cmd.Stdout, cmd.Stderr = &out, &errb
	start := time.Now()
	err := cmd.Start()
	if err != nil {
		return Result{Err: fmt.Errorf("cannot start %q: %w", s.Args[0], err), ExitCode: -1}
	}
	track(cmd, true)
	waitErr := cmd.Wait()
	killGroup(cmd)
	track(cmd, false)
	r := Result{Stdout: out.buf.Bytes(), Stderr: errb.buf.Bytes(), Duration: time.Since(start), Truncated: out.cut || errb.cut}
	r.ExitCode = cmd.ProcessState.ExitCode()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		r.TimedOut = true
		r.Err = fmt.Errorf("timed out after %s", s.Timeout)
	case ctx.Err() != nil:
		r.Cancelled = true
		r.Err = errors.New("cancelled")
	default:
		var ee *exec.ExitError
		if waitErr != nil && !errors.As(waitErr, &ee) {
			r.Err = waitErr
		}
	}
	return r
}

var (
	activeMu sync.Mutex
	active   = map[*exec.Cmd]bool{}
)

func track(cmd *exec.Cmd, on bool) {
	activeMu.Lock()
	defer activeMu.Unlock()
	if on {
		active[cmd] = true
	} else {
		delete(active, cmd)
	}
}

// KillAll kills, synchronously, every command still running (and its
// process group). Called when the program exits so no project code keeps
// running behind it.
func KillAll() {
	activeMu.Lock()
	defer activeMu.Unlock()
	for cmd := range active {
		killGroup(cmd)
	}
}

// ArtifactPath names a new directory for files produced for a repository
// (profiles, traces, coverage), in the user cache directory (or
// GITVIEW_CACHE_DIR) so the analysed working tree is never modified. It is created by ArtifactDir or by the
// caller once the user has agreed to run something.
func ArtifactPath(repoRoot, kind string) string {
	base := os.Getenv("GITVIEW_CACHE_DIR")
	if base == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			cache = os.TempDir()
		}
		base = filepath.Join(cache, "gitview")
	}
	sum := sha256.Sum256([]byte(repoRoot))
	parent := filepath.Join(base, hex.EncodeToString(sum[:6]))
	prune(parent, keepArtifacts)
	return filepath.Join(parent, kind+"-"+time.Now().Format("20060102-150405.000"))
}

// keepArtifacts bounds how many artifact directories a repository keeps.
const keepArtifacts = 40

// prune removes the oldest artifact directories beyond keep (names start
// with the kind and end with a sortable timestamp).
func prune(parent string, keep int) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	type dir struct{ name, stamp string }
	var dirs []dir
	for _, e := range entries {
		if i := strings.IndexByte(e.Name(), '-'); e.IsDir() && i > 0 {
			dirs = append(dirs, dir{e.Name(), e.Name()[i+1:]})
		}
	}
	if len(dirs) <= keep {
		return
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].stamp < dirs[j].stamp })
	for _, d := range dirs[:len(dirs)-keep] {
		os.RemoveAll(filepath.Join(parent, d.name))
	}
}

// ArtifactDir creates and returns a new artifact directory.
func ArtifactDir(repoRoot, kind string) (string, error) {
	dir := ArtifactPath(repoRoot, kind)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// InsideRoot checks that dir is the repository root or below it.
func InsideRoot(root, dir string) error {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("%s is outside the repository", dir)
	}
	return nil
}
