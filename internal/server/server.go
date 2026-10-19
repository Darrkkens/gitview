// Package server is the local HTTP server of `gitview serve`: the web UI
// (internal/webui) and a small JSON API over the analyses of one
// repository. It listens on 127.0.0.1 only, and every API call must carry
// the random token of the URL it printed; requests addressed to another
// host name (DNS rebinding) or from another origin are refused.
//
// The analyses themselves are not this package's concern: the command
// registers one Analyzer per page section and computes them on demand.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Analyzer computes one section of the page (any JSON-marshalable value).
// It runs with the server's context, not a request's, so closing the page
// does not cancel a long analysis.
type Analyzer func(ctx context.Context) (any, error)

// APIFunc is an extra endpoint: it gets the request and its JSON body (POST,
// at most 64 KiB) and returns the value to encode, or an error with an HTTP
// status (0 means 500 for errors, 200 otherwise).
type APIFunc func(r *http.Request, body []byte) (any, int, error)

// Config describes what the server serves.
type Config struct {
	Repository string // name shown by the page
	Root       string // repository root (shown on hover)
	Branch     func() string
	// View is the view the page opens on (e.g. "security"); "" for the app's default.
	View string
	// Sections are computed on first request, in the order the page asks.
	Sections map[string]Analyzer
	// Order lists the section names in the order to analyse them ahead of
	// time (Warm); sections not listed are only computed on demand.
	Order []string
	// Sources returns the base64 gzip bundle of the Go files the code views
	// show (see internal/graph/export.SourceBundle), "" when none.
	Sources func(ctx context.Context) (string, error)
	// API holds extra endpoints by pattern, e.g. "POST /api/review".
	API map[string]APIFunc
	// Status adds fields to /api/status (e.g. the AI model's availability).
	Status func(ctx context.Context) map[string]any
	// Changes signals working-tree changes; the page offers a re-analysis.
	Changes <-chan struct{}
	// Token replaces the random token (development: a stable URL for the
	// Vite dev server); "" draws one.
	Token string
	// LayoutFile is where GET and PUT /api/layout keep the card positions of
	// the Architecture view (outside the repository); "" disables them.
	LayoutFile string
	// ParseLayout validates a layout document before it is stored.
	ParseLayout func([]byte) (json.RawMessage, error)
	// MaxLayout bounds a stored layout document.
	MaxLayout int64
}

// Server serves one repository.
type Server struct {
	cfg      Config
	token    string
	host     string // 127.0.0.1:port
	ctx      context.Context
	sections map[string]*section
	events   *broker
	changed  time.Time
	mu       sync.Mutex
	layoutMu sync.Mutex
}

// New prepares a server; Listen binds it.
func New(cfg Config) *Server {
	s := &Server{cfg: cfg, token: cfg.Token, sections: map[string]*section{}, events: newBroker()}
	if s.token == "" {
		s.token = newToken()
	}
	for name, fn := range cfg.Sections {
		s.sections[name] = &section{name: name, fn: fn, notify: s.sectionChanged}
	}
	return s
}

func newToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// Token is the secret every API call carries.
func (s *Server) Token() string { return s.token }

// URL is the address to open (with the token), valid once listening.
func (s *Server) URL() string { return "http://" + s.host + "/?t=" + s.token }

// Listen binds 127.0.0.1:port (0 picks a free port).
func (s *Server) Listen(port int) (net.Listener, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	s.host = ln.Addr().String()
	return ln, nil
}

// Serve runs until ctx is done, then shuts down gracefully.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.ctx = ctx
	go s.watch(ctx)
	hs := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	go func() {
		<-ctx.Done()
		s.events.close()
		sh, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		hs.Shutdown(sh)
	}()
	if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Warm starts the analyses listed in Config.Order in the background, one
// after the other, so the views open without waiting.
func (s *Server) Warm(ctx context.Context) {
	go func() {
		for _, name := range s.cfg.Order {
			if sec := s.sections[name]; sec != nil && ctx.Err() == nil {
				sec.get(ctx, ctx)
			}
		}
	}()
}

func (s *Server) watch(ctx context.Context) {
	if s.cfg.Changes == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-s.cfg.Changes:
			if !ok {
				return
			}
			now := time.Now()
			s.mu.Lock()
			s.changed = now
			s.mu.Unlock()
			for _, sec := range s.sections {
				sec.markStale()
			}
			s.events.publish("changed", map[string]any{"at": now.Format(time.RFC3339)})
		}
	}
}

func (s *Server) sectionChanged(name string, st sectionStatus) {
	s.events.publish("section", map[string]any{"name": name, "state": st.State, "error": st.Error})
}

// ---------- sections ----------

type sectionStatus struct {
	State    string     `json:"state"` // idle | running | ready | error
	Error    string     `json:"error,omitempty"`
	Started  *time.Time `json:"started,omitempty"`
	Finished *time.Time `json:"finished,omitempty"`
	Stale    bool       `json:"stale,omitempty"`
}

type section struct {
	name   string
	fn     Analyzer
	notify func(string, sectionStatus)

	mu      sync.Mutex
	running chan struct{} // closed when the current run ends
	data    any
	err     error
	started time.Time
	done    time.Time
	stale   bool
	gen     int
}

func (sec *section) status() sectionStatus {
	sec.mu.Lock()
	defer sec.mu.Unlock()
	return sec.statusLocked()
}

func (sec *section) statusLocked() sectionStatus {
	st := sectionStatus{State: "idle", Stale: sec.stale}
	if !sec.started.IsZero() {
		t := sec.started
		st.Started = &t
	}
	switch {
	case sec.running != nil:
		st.State = "running"
	case sec.err != nil:
		st.State, st.Error = "error", sec.err.Error()
	case sec.data != nil:
		st.State = "ready"
	}
	if !sec.done.IsZero() && sec.running == nil {
		t := sec.done
		st.Finished = &t
	}
	return st
}

// get returns the section, computing it with run (the server's context) if
// needed and waiting for it while wait (the request's context) lasts.
func (sec *section) get(run, wait context.Context) (any, error) {
	sec.mu.Lock()
	if sec.running == nil && sec.data == nil && sec.err == nil {
		sec.startLocked(run)
	}
	ch := sec.running
	sec.mu.Unlock()
	if ch != nil {
		select {
		case <-ch:
		case <-wait.Done():
			return nil, wait.Err()
		}
	}
	sec.mu.Lock()
	defer sec.mu.Unlock()
	return sec.data, sec.err
}

// refresh drops the result and analyses again.
func (sec *section) refresh(run context.Context) {
	sec.mu.Lock()
	defer sec.mu.Unlock()
	if sec.running != nil {
		return // already analysing the current tree
	}
	sec.data, sec.err, sec.stale = nil, nil, false
	sec.startLocked(run)
}

func (sec *section) markStale() {
	sec.mu.Lock()
	sec.stale = sec.data != nil || sec.err != nil
	sec.mu.Unlock()
}

func (sec *section) startLocked(ctx context.Context) {
	ch := make(chan struct{})
	sec.running, sec.started, sec.stale = ch, time.Now(), false
	sec.gen++
	gen := sec.gen
	go func() {
		data, err := sec.fn(ctx)
		if err == nil && data == nil {
			err = errors.New("no data")
		}
		sec.mu.Lock()
		if sec.gen == gen {
			sec.data, sec.err, sec.done = data, err, time.Now()
			sec.running = nil
		}
		st := sec.statusLocked()
		sec.mu.Unlock()
		close(ch)
		sec.notify(sec.name, st)
	}()
	go sec.notify(sec.name, sectionStatus{State: "running"})
}

// ---------- server-sent events ----------

type event struct {
	name string
	data any
}

type broker struct {
	mu     sync.Mutex
	subs   map[chan event]struct{}
	closed bool
}

func newBroker() *broker { return &broker{subs: map[chan event]struct{}{}} }

func (b *broker) subscribe() chan event {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan event, 16)
	if b.closed {
		close(ch)
		return ch
	}
	b.subs[ch] = struct{}{}
	return ch
}

func (b *broker) unsubscribe(ch chan event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
		close(ch)
	}
}

func (b *broker) publish(name string, data any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- event{name, data}:
		default: // a slow page misses an event; the next status poll catches up
		}
	}
}

func (b *broker) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for ch := range b.subs {
		delete(b.subs, ch)
		close(ch)
	}
}
