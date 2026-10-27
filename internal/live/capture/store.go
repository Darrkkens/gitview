// Package capture runs the capture sessions of the Runtime Performance
// Inspector — process metrics sampled from /proc, HTTP requests from the
// proxy, the collector or OpenTelemetry, and optional profiles — and keeps
// their history on disk.
package capture

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/athos/gitview/internal/live/goprof"
	"github.com/athos/gitview/internal/live/httpcol"
	"github.com/athos/gitview/internal/live/linuxperf"
	"github.com/athos/gitview/internal/live/proc"
)

// FormatVersion is the version of capture.json.
const FormatVersion = 1

// Status of a finished capture.
const (
	StatusComplete  = "complete"
	StatusExited    = "process exited"
	StatusRestarted = "process restarted"
	StatusCancelled = "cancelled"
	StatusFailed    = "failed"
)

// ProcessInfo identifies the monitored process (never its command line).
type ProcessInfo struct {
	PID         int               `json:"pid"`
	Comm        string            `json:"comm"`
	Exe         string            `json:"exe,omitempty"`
	ExeDeleted  bool              `json:"exeDeleted,omitempty"`
	StartTicks  uint64            `json:"startTicks"`
	Started     time.Time         `json:"started"`
	Go          bool              `json:"go"`
	GoVersion   string            `json:"goVersion,omitempty"`
	MainModule  string            `json:"mainModule,omitempty"`
	MainVersion string            `json:"mainVersion,omitempty"`
	VCSRevision string            `json:"vcsRevision,omitempty"`
	VCSModified bool              `json:"vcsModified,omitempty"`
	Ports       []proc.Port       `json:"ports,omitempty"`
	Unavailable map[string]string `json:"unavailable,omitempty"` // metric → reason
	CPUs        int               `json:"cpus"`                  // CPUs of the machine
}

// Phases places the phases of the capture on its timeline (offsets from
// the start of the baseline, monotonic clock).
type Phases struct {
	ActiveStart time.Duration   `json:"activeStart"`
	ActiveEnd   time.Duration   `json:"activeEnd"`
	End         time.Duration   `json:"end"`
	Paused      []goprof.Window `json:"paused,omitempty"`
}

// Active is the length of the active phase, pauses excluded.
func (p Phases) Active() time.Duration {
	d := p.ActiveEnd - p.ActiveStart
	for _, w := range p.Paused {
		d -= w.End - w.Start
	}
	return max(d, 0)
}

// InActive reports whether t is in the active phase and not paused.
func (p Phases) InActive(t time.Duration) bool {
	if t < p.ActiveStart || t > p.ActiveEnd {
		return false
	}
	for _, w := range p.Paused {
		if t >= w.Start && t <= w.End {
			return false
		}
	}
	return true
}

// Settings records how the capture was made.
type Settings struct {
	Interval    time.Duration `json:"interval"`
	Baseline    time.Duration `json:"baseline"`
	Duration    time.Duration `json:"duration,omitempty"` // 0: until stopped
	Cooldown    time.Duration `json:"cooldown"`
	MaxDuration time.Duration `json:"maxDuration"`
	Proxy       string        `json:"proxy,omitempty"`  // proxy address
	Target      string        `json:"target,omitempty"` // application address behind the proxy
	Collector   string        `json:"collector,omitempty"`
	OTLP        string        `json:"otlp,omitempty"`
	Endpoint    string        `json:"endpoint,omitempty"` // route filter chosen by the user
	Profile     goprof.Plan   `json:"profile"`
	Perf        string        `json:"perf,omitempty"` // auto | on | off
}

// Capture is a finished capture as stored in capture.json.
type Capture struct {
	Version    int                  `json:"version"`
	ID         string               `json:"id"`
	Label      string               `json:"label,omitempty"`
	Created    time.Time            `json:"created"`
	Status     string               `json:"status"`
	StatusNote string               `json:"statusNote,omitempty"`
	Repo       string               `json:"repo,omitempty"`
	Process    ProcessInfo          `json:"process"`
	Settings   Settings             `json:"settings"`
	Phases     Phases               `json:"phases"`
	Samples    []proc.Sample        `json:"samples"`
	Requests   []httpcol.Request    `json:"requests"`
	Orphans    []httpcol.OrphanSpan `json:"orphans,omitempty"`
	OTelSpans  int                  `json:"otelSpans,omitempty"`
	Lost       int64                `json:"lost,omitempty"` // requests dropped by the collector's buffer
	Profiles   *goprof.Result       `json:"profiles,omitempty"`
	Perf       *linuxperf.Result    `json:"perf,omitempty"`    // CPU sampled by Linux perf
	TraceAt    time.Time            `json:"traceAt,omitempty"` // wall time the trace was requested
	Sources    []string             `json:"sources"`
	Warnings   []string             `json:"warnings,omitempty"`
	Notes      []string             `json:"notes,omitempty"`

	Dir string `json:"-"` // where it is stored
}

// Limits bound the history and the captures.
type Limits struct {
	MaxCaptures  int
	MaxBytes     int64
	MaxDuration  time.Duration
	MinInterval  time.Duration
	MaxInterval  time.Duration
	DefaultEvery time.Duration
}

// DefaultLimits are GitView's own limits; a repository's configuration
// can only lower them.
var DefaultLimits = Limits{
	MaxCaptures:  30,
	MaxBytes:     512 << 20,
	MaxDuration:  10 * time.Minute,
	MinInterval:  50 * time.Millisecond,
	MaxInterval:  5 * time.Second,
	DefaultEvery: 250 * time.Millisecond,
}

// Store is the capture history of one repository (or of captures made
// outside any repository).
type Store struct {
	Dir    string
	Limits Limits
}

// StoreFor returns the store of a repository root ("" outside a
// repository), in GitView's cache directory (GITVIEW_CACHE_DIR overrides).
func StoreFor(repoRoot string) (*Store, error) {
	base := os.Getenv("GITVIEW_CACHE_DIR")
	if base == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return nil, err
		}
		base = filepath.Join(cache, "gitview")
	}
	key := "no-repository"
	if repoRoot != "" {
		sum := sha256.Sum256([]byte(repoRoot))
		key = hex.EncodeToString(sum[:6])
	}
	return &Store{Dir: filepath.Join(base, key, "runtime"), Limits: DefaultLimits}, nil
}

func newID(t time.Time) string {
	var b [3]byte
	rand.Read(b[:])
	return t.Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}

// NewDir creates the directory of a new capture.
func (s *Store) NewDir(t time.Time) (id, dir string, err error) {
	id = newID(t)
	dir = filepath.Join(s.Dir, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	return id, dir, nil
}

// Save writes capture.json and prunes the history.
func (s *Store) Save(c *Capture) error {
	if c.Dir == "" {
		return errors.New("capture without directory")
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp := filepath.Join(c.Dir, "capture.json.tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(c.Dir, "capture.json")); err != nil {
		return err
	}
	s.Prune(c.ID)
	return nil
}

// Load reads a capture by ID (a unique prefix is enough) or directory.
func (s *Store) Load(ref string) (*Capture, error) {
	dir := ref
	if !strings.ContainsRune(ref, os.PathSeparator) {
		id, err := s.resolve(ref)
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(s.Dir, id)
	}
	data, err := os.ReadFile(filepath.Join(dir, "capture.json"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("no capture in %s", dir)
		}
		return nil, err
	}
	var c Capture
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(dir, "capture.json"), err)
	}
	if c.Version > FormatVersion {
		return nil, fmt.Errorf("capture %s was written by a newer GitView (format %d)", c.ID, c.Version)
	}
	c.Dir = dir
	return &c, nil
}

func (s *Store) resolve(ref string) (string, error) {
	entries, _ := os.ReadDir(s.Dir)
	var found []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), ref) {
			if _, err := os.Stat(filepath.Join(s.Dir, e.Name(), "capture.json")); err == nil {
				found = append(found, e.Name())
			}
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no capture %q in the history", ref)
	case 1:
		return found[0], nil
	}
	for _, f := range found {
		if f == ref {
			return f, nil
		}
	}
	return "", fmt.Errorf("%q matches %d captures; give more characters", ref, len(found))
}

// Entry is a capture in the history list.
type Entry struct {
	ID       string        `json:"id"`
	Label    string        `json:"label,omitempty"`
	Created  time.Time     `json:"created"`
	Status   string        `json:"status"`
	PID      int           `json:"pid"`
	Exe      string        `json:"exe,omitempty"`
	Active   time.Duration `json:"active"`
	Requests int           `json:"requests"`
	Routes   []string      `json:"routes,omitempty"`
	Bytes    int64         `json:"bytes"`
}

// List returns the history, newest first.
func (s *Store) List() ([]Entry, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		c, err := s.Load(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			continue
		}
		en := Entry{ID: c.ID, Label: c.Label, Created: c.Created, Status: c.Status, PID: c.Process.PID, Exe: c.Process.Exe,
			Active: c.Phases.Active(), Requests: len(c.Requests), Bytes: dirSize(c.Dir)}
		seen := map[string]bool{}
		for _, r := range c.Requests {
			if !seen[r.Key] {
				seen[r.Key] = true
				en.Routes = append(en.Routes, r.Key)
			}
		}
		sort.Strings(en.Routes)
		out = append(out, en)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

// Delete removes a capture.
func (s *Store) Delete(id string) error {
	id, err := s.resolve(id)
	if err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(s.Dir, id))
}

func dirSize(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

// Prune removes the oldest captures beyond the count and size limits,
// never the one named keep, and directories left by interrupted captures.
func (s *Store) Prune(keep string) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return
	}
	type item struct {
		id    string
		size  int64
		valid bool
		mod   time.Time
	}
	var items []item
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(s.Dir, e.Name())
		_, err := os.Stat(filepath.Join(dir, "capture.json"))
		info, _ := e.Info()
		it := item{id: e.Name(), size: dirSize(dir), valid: err == nil}
		if info != nil {
			it.mod = info.ModTime()
		}
		// A directory without capture.json older than the longest capture
		// is what an interrupted GitView left behind.
		if !it.valid && e.Name() != keep && time.Since(it.mod) > s.Limits.MaxDuration+5*time.Minute {
			os.RemoveAll(dir)
			continue
		}
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].id > items[j].id }) // newest first (IDs start with the time)
	var total int64
	count := 0
	for _, it := range items {
		if !it.valid {
			continue
		}
		count++
		total += it.size
		if it.id == keep {
			continue
		}
		if count > s.Limits.MaxCaptures || total > s.Limits.MaxBytes {
			os.RemoveAll(filepath.Join(s.Dir, it.id))
			total -= it.size
			count--
		}
	}
}

// enforceSize removes optional artifacts of a capture, largest kinds
// first (trace, then snapshots, then CPU windows), until its directory fits
// in max bytes (capture.json itself is small). It returns what was removed.
func enforceSize(c *Capture, max int64) []string {
	if (c.Profiles == nil && c.Perf == nil) || dirSize(c.Dir) <= max {
		return nil
	}
	var dropped []string
	drop := func(w *goprof.Window, what string) {
		if w == nil || w.File == "" || dirSize(c.Dir) <= max {
			return
		}
		os.Remove(filepath.Join(c.Dir, w.File))
		w.File, w.Err = "", "removed: the capture exceeded the storage limit"
		dropped = append(dropped, what)
	}
	if p := c.Profiles; p != nil {
		drop(p.Trace, "the execution trace")
		for _, w := range []*goprof.Window{p.HeapStart, p.HeapEnd} {
			drop(w, "heap snapshots")
		}
		for _, w := range []*goprof.Window{p.Goroutine, p.MutexStart, p.MutexEnd, p.BlockStart, p.BlockEnd} {
			drop(w, "goroutine/mutex/block snapshots")
		}
		for i := len(p.CPU) - 1; i >= 0; i-- {
			drop(&p.CPU[i], "CPU profile windows")
		}
	}
	if c.Perf != nil && c.Perf.File != "" && dirSize(c.Dir) > max {
		os.Remove(filepath.Join(c.Dir, c.Perf.File))
		c.Perf.File, c.Perf.Error = "", "removed: the capture exceeded the storage limit"
		dropped = append(dropped, "the perf samples")
	}
	// Each kind once in the message.
	seen := map[string]bool{}
	out := dropped[:0]
	for _, d := range dropped {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}
