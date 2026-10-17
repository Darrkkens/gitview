// Package collector is GitView's optional in-process instrumentation for Go
// HTTP servers. It lets GitView's Runtime Performance Inspector correlate
// the requests sent to the server during a capture (method, route, status,
// timing, sizes, SQL statements) and attribute CPU profile samples to them.
//
// It uses only the standard library and does nothing until GitView arms it
// for a capture: an idle collector costs one atomic load per request.
//
//	c := collector.Default()
//	http.ListenAndServe(":8080", c.Middleware(mux))
//	c.Start("") // GitView's endpoints, on a random 127.0.0.1 port
//
// What is never recorded: headers (cookies, Authorization…), query strings,
// request and response bodies, SQL parameter values and SQL literals.
// Setting GITVIEW_COLLECTOR=off disables it entirely.
package collector

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"runtime/pprof"
	"runtime/trace"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// HeaderTraceID carries the request identifier between the client,
	// GitView's proxy and the collector.
	HeaderTraceID = "X-GitView-Trace-ID"
	// BasePath is where the collector's endpoints are served.
	BasePath = "/debug/gitview/"
	// EnvDisable set to "off" disables the collector.
	EnvDisable = "GITVIEW_COLLECTOR"
	// Version of the JSON protocol spoken with GitView.
	Version = 1

	// LabelTraceID is the pprof label set on the goroutines serving a
	// request while a capture is armed.
	LabelTraceID = "gitview_trace_id"
	// TaskName is the runtime/trace task of each request.
	TaskName = "gitview.http"
)

// Options configures a collector.
type Options struct {
	// Capacity is the number of completed requests kept (default 4096).
	Capacity int
	// MaxSQL is the number of SQL statements kept per request (default 256).
	MaxSQL int
	// Disabled turns the collector into a no-op.
	Disabled bool
}

// Collector records requests while a GitView capture is armed.
type Collector struct {
	opts     Options
	disabled bool
	token    string
	started  time.Time

	armed    atomic.Pointer[armState]
	inFlight atomic.Int64

	mu        sync.Mutex
	ring      []Request
	seen      map[string]bool // request IDs used recently
	seenOrder []string
	next      int64 // sequence of the next completed request
	orphans   []SQLSpan
	orphSeq   int64

	profMu      sync.Mutex // one profile request at a time
	rates       rates
	announced   string // announcement file to remove on Close
	closers     []func() error
	handlerOnce sync.Once
}

type armState struct {
	until   time.Time
	labels  bool
	tasks   bool
	runtime bool
	lease   string
}

var (
	defaultOnce sync.Once
	defaultColl *Collector
)

// Default returns the process-wide collector.
func Default() *Collector {
	defaultOnce.Do(func() { defaultColl = New(Options{}) })
	return defaultColl
}

// Middleware wraps next with the default collector.
func Middleware(next http.Handler) http.Handler { return Default().Middleware(next) }

// New creates a collector.
func New(opts Options) *Collector {
	if opts.Capacity <= 0 {
		opts.Capacity = 4096
	}
	if opts.MaxSQL <= 0 {
		opts.MaxSQL = 256
	}
	c := &Collector{opts: opts, started: time.Now()}
	c.disabled = opts.Disabled || strings.EqualFold(os.Getenv(EnvDisable), "off")
	var b [24]byte
	rand.Read(b[:])
	c.token = hex.EncodeToString(b[:])
	return c
}

// Disabled reports whether the collector was turned off.
func (c *Collector) Disabled() bool { return c.disabled }

func (c *Collector) state() *armState {
	s := c.armed.Load()
	if s == nil {
		return nil
	}
	if time.Now().After(s.until) {
		c.disarm()
		return nil
	}
	return s
}

// Request is a completed request, as served to GitView.
type Request struct {
	Seq           int64     `json:"seq"`
	ID            string    `json:"id"`            // unique per request
	W3C           string    `json:"w3c,omitempty"` // trace ID of an incoming traceparent
	Method        string    `json:"method"`
	Path          string    `json:"path"`
	Route         string    `json:"route,omitempty"`
	Status        int       `json:"status"`
	Start         int64     `json:"start"` // Unix nanoseconds
	Dur           int64     `json:"dur"`   // nanoseconds, monotonic clock
	BytesIn       int64     `json:"bytesIn"`
	ContentLength int64     `json:"contentLength"`
	BytesOut      int64     `json:"bytesOut"`
	InFlight      int64     `json:"inFlight"` // requests being served when it started, itself included
	Panic         bool      `json:"panic,omitempty"`
	Labelled      bool      `json:"labelled,omitempty"`
	SQL           []SQLSpan `json:"sql,omitempty"`
	SQLDropped    int       `json:"sqlDropped,omitempty"`
	Runtime       *Counters `json:"runtime,omitempty"`
}

// requestState is the record of a request being served.
type requestState struct {
	c        *Collector
	req      Request
	start    time.Time
	mu       sync.Mutex
	route    string
	sql      []SQLSpan
	dropped  int
	finished bool
}

type ctxKey struct{}

func fromContext(ctx context.Context) *requestState {
	if ctx == nil {
		return nil
	}
	rs, _ := ctx.Value(ctxKey{}).(*requestState)
	return rs
}

// SetRoute records the route pattern of the request in ctx, for routers
// other than net/http's ServeMux (whose pattern is read automatically).
func SetRoute(ctx context.Context, pattern string) {
	if rs := fromContext(ctx); rs != nil {
		rs.mu.Lock()
		rs.route = pattern
		rs.mu.Unlock()
	}
}

// TraceID returns the GitView trace ID of the request in ctx, or "".
func TraceID(ctx context.Context) string {
	if rs := fromContext(ctx); rs != nil {
		return rs.req.ID
	}
	return ""
}

// Middleware records the requests served by next while a capture is armed.
func (c *Collector) Middleware(next http.Handler) http.Handler {
	if c.disabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := c.state()
		if st == nil || strings.HasPrefix(r.URL.Path, BasePath) {
			next.ServeHTTP(w, r)
			return
		}
		c.serve(st, next, w, r)
	})
}

func (c *Collector) serve(st *armState, next http.Handler, w http.ResponseWriter, r *http.Request) {
	id, w3c := c.incomingID(r)
	rs := &requestState{c: c, start: time.Now()}
	rs.req = Request{
		ID:            id,
		W3C:           w3c,
		Method:        r.Method,
		Path:          MaskPath(r.URL.Path),
		Start:         rs.start.UnixNano(),
		ContentLength: r.ContentLength,
		InFlight:      c.inFlight.Add(1),
	}
	defer c.inFlight.Add(-1)
	var before Counters
	if st.runtime {
		before = readCounters()
	}
	ctx := context.WithValue(r.Context(), ctxKey{}, rs)
	if st.tasks {
		var task *trace.Task
		ctx, task = trace.NewTask(ctx, TaskName)
		trace.Log(ctx, "gitview.trace_id", id)
		trace.Log(ctx, "gitview.request", r.Method+" "+rs.req.Path)
		defer task.End()
	}
	if st.labels {
		ctx = pprof.WithLabels(ctx, pprof.Labels(LabelTraceID, id))
		pprof.SetGoroutineLabels(ctx)
		defer pprof.SetGoroutineLabels(r.Context())
		rs.req.Labelled = true
	}
	r2 := r.WithContext(ctx)
	var body *countingBody
	if r.Body != nil && r.Body != http.NoBody {
		body = &countingBody{rc: r.Body}
		r2.Body = body
	}
	w.Header().Set(HeaderTraceID, id)
	rw := &responseWriter{w: w}
	panicked := true
	defer func() {
		if panicked {
			rs.req.Panic = true
			if rw.status == 0 {
				rw.status = http.StatusInternalServerError
			}
		}
		rs.req.Dur = int64(time.Since(rs.start))
		rs.req.Status = rw.status
		if rs.req.Status == 0 {
			rs.req.Status = http.StatusOK
		}
		rs.req.BytesOut = rw.n
		if body != nil {
			rs.req.BytesIn = body.n.Load()
		}
		if st.runtime {
			d := readCounters().sub(before)
			rs.req.Runtime = &d
		}
		rs.mu.Lock()
		rs.req.Route = rs.route
		if rs.req.Route == "" {
			rs.req.Route = r2.Pattern
		}
		rs.req.SQL, rs.req.SQLDropped = rs.sql, rs.dropped
		rs.finished = true
		rs.mu.Unlock()
		c.store(rs.req)
	}()
	next.ServeHTTP(rw, r2)
	panicked = false
}

// incomingID returns the request ID set by GitView's proxy
// (X-GitView-Trace-ID), unless it was already used by another request, and
// a new one otherwise; and the W3C trace ID of a traceparent header, which
// identifies a trace (possibly many requests), never a request.
func (c *Collector) incomingID(r *http.Request) (id, w3c string) {
	if tp := r.Header.Get("traceparent"); len(tp) >= 55 && tp[2] == '-' {
		if t := tp[3:35]; ValidID(t) && t != strings.Repeat("0", 32) {
			w3c = strings.ToLower(t)
		}
	}
	id = r.Header.Get(HeaderTraceID)
	if !ValidID(id) || !c.claim(id) {
		id = NewID()
		c.claim(id)
	}
	return id, w3c
}

// claim records an ID as used; it reports false when it already was (a
// client repeating the same header must not merge its requests).
func (c *Collector) claim(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen == nil {
		c.seen = map[string]bool{}
	}
	if c.seen[id] {
		return false
	}
	c.seen[id] = true
	c.seenOrder = append(c.seenOrder, id)
	if len(c.seenOrder) > 2*c.opts.Capacity {
		for _, old := range c.seenOrder[:c.opts.Capacity] {
			delete(c.seen, old)
		}
		c.seenOrder = append([]string(nil), c.seenOrder[c.opts.Capacity:]...)
	}
	return true
}

// NewID returns a new 32-hex-digit trace ID (W3C trace-id compatible).
func NewID() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// ValidID accepts 8 to 64 characters of [A-Za-z0-9._-].
func ValidID(id string) bool {
	if len(id) < 8 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func (c *Collector) store(r Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	r.Seq = c.next
	if len(c.ring) < c.opts.Capacity {
		c.ring = append(c.ring, r)
		return
	}
	c.ring[int((r.Seq-1)%int64(c.opts.Capacity))] = r
}

// since returns up to max requests with a sequence above seq, in order,
// and how many were lost because the ring overflowed.
func (c *Collector) since(seq int64, max int) (out []Request, lost int64, last int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	oldest := c.next - int64(len(c.ring)) + 1
	if seq+1 < oldest {
		lost = oldest - (seq + 1)
		seq = oldest - 1
	}
	for s := seq + 1; s <= c.next && len(out) < max; s++ {
		out = append(out, c.ring[int((s-1)%int64(c.opts.Capacity))])
	}
	last = c.next
	return out, lost, last
}

type countingBody struct {
	rc io.ReadCloser
	n  atomic.Int64
}

func (b *countingBody) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	b.n.Add(int64(n))
	return n, err
}

func (b *countingBody) Close() error { return b.rc.Close() }

// responseWriter counts the response and keeps the optional interfaces of
// the wrapped writer reachable (http.ResponseController uses Unwrap).
type responseWriter struct {
	w      http.ResponseWriter
	status int
	n      int64
}

func (w *responseWriter) Header() http.Header { return w.w.Header() }

func (w *responseWriter) WriteHeader(code int) {
	if w.status == 0 && code >= 200 {
		w.status = code
	}
	w.w.WriteHeader(code)
}

func (w *responseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.w.Write(p)
	w.n += int64(n)
	return n, err
}

func (w *responseWriter) Flush() {
	if f, ok := w.w.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.w }

// MaskPath keeps only the path segments that are plainly words ("clients",
// "v2", "oauth2", "how-to-configure"): every other segment is replaced by a
// placeholder — "{n}" for numbers, "{uuid}", "{hex}", "{value}" for the
// rest — so identifiers, codes and tokens are never recorded.
func MaskPath(p string) string {
	if !strings.Contains(p, "/") {
		return p
	}
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = maskSegment(s)
	}
	return strings.Join(parts, "/")
}

func maskSegment(s string) string {
	switch {
	case s == "" || isWord(s) || strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") && len(s) <= 40:
		return s
	case isDigits(s):
		return "{n}"
	case isUUID(s):
		return "{uuid}"
	case len(s) >= 8 && isHex(s):
		return "{hex}"
	}
	return "{value}"
}

// isWord accepts runs of letters joined by single '-', '_' or '.', each run
// at most 16 letters with few case changes, optionally ending in up to two
// digits after at least two letters ("v2", "oauth2", "ipv6"; not "s3").
func isWord(s string) bool {
	if len(s) > 64 {
		return false
	}
	if len(s) >= 2 && len(s) <= 4 && (s[0] == 'v' || s[0] == 'V') && isDigits(s[1:]) {
		return true // API version: v1, v12
	}
	body := strings.TrimRight(s, "0123456789")
	if n := len(s) - len(body); n > 2 || n > 0 && len(body) < 2 {
		return false
	}
	run, changes := 0, 0
	prevUpper, prevSep := false, true
	for i, r := range body {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			upper := r <= 'Z'
			if run > 0 && upper != prevUpper {
				changes++
			}
			prevUpper = upper
			run++
			if run > 16 || changes > 3 {
				return false
			}
			prevSep = false
		case r == '-' || r == '_' || r == '.':
			if prevSep || i == len(body)-1 {
				return false
			}
			prevSep, run, changes = true, 0, 0
		default:
			return false
		}
	}
	return body != ""
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func isHex(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return s != ""
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
		} else if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}
