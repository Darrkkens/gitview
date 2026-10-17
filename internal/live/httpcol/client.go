package httpcol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/athos/gitview/internal/live/proc"
)

// The collector's JSON protocol (github.com/athos/gitview/collector,
// version 1).

type announcement struct {
	Collector string `json:"collector"`
	Version   int    `json:"version"`
	PID       int    `json:"pid"`
	Addr      string `json:"addr"`
	Path      string `json:"path"`
	Token     string `json:"token"`
	Started   int64  `json:"started"`
}

// CollectorInfo is the collector's description of itself.
type CollectorInfo struct {
	Collector string `json:"collector"`
	Version   int    `json:"version"`
	PID       int    `json:"pid"`
	GoVersion string `json:"goVersion"`
	Started   int64  `json:"started"`
	Capacity  int    `json:"capacity"`
	Armed     bool   `json:"armed"`
	Until     int64  `json:"until"`
	Last      int64  `json:"last"`
	InFlight  int64  `json:"inFlight"`
	Lease     string `json:"lease"`
}

type wireSpan struct {
	Query      string `json:"query"`
	Start      int64  `json:"start"`
	Dur        int64  `json:"dur"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Func       string `json:"func"`
	Rows       int64  `json:"rows"`
	HasRows    bool   `json:"hasRows"`
	Err        string `json:"err"`
	Unfinished bool   `json:"unfinished"`
	TraceID    string `json:"traceId"`
}

type wireRequest struct {
	Seq           int64      `json:"seq"`
	ID            string     `json:"id"`
	W3C           string     `json:"w3c"`
	Method        string     `json:"method"`
	Path          string     `json:"path"`
	Route         string     `json:"route"`
	Status        int        `json:"status"`
	Start         int64      `json:"start"`
	Dur           int64      `json:"dur"`
	BytesIn       int64      `json:"bytesIn"`
	ContentLength int64      `json:"contentLength"`
	BytesOut      int64      `json:"bytesOut"`
	InFlight      int64      `json:"inFlight"`
	Panic         bool       `json:"panic"`
	Labelled      bool       `json:"labelled"`
	SQL           []wireSpan `json:"sql"`
	SQLDropped    int        `json:"sqlDropped"`
	Runtime       *struct {
		GCCycles uint64 `json:"gcCycles"`
	} `json:"runtime"`
}

type wireRequests struct {
	PID         int           `json:"pid"`
	Now         int64         `json:"now"`
	Last        int64         `json:"last"`
	Lost        int64         `json:"lost"`
	More        bool          `json:"more"`
	Requests    []wireRequest `json:"requests"`
	Orphans     []wireSpan    `json:"orphans"`
	OrphansLast int64         `json:"orphansLast"`
}

// OrphanSpan is a SQL statement executed outside any recorded request
// (background work).
type OrphanSpan struct {
	Start time.Time `json:"start"`
	SQLSpan
}

// Page is one read of the collector's completed requests.
type Page struct {
	Requests    []Request
	Orphans     []OrphanSpan
	Last        int64 // pass back as since
	OrphansLast int64
	Lost        int64 // requests that overflowed the collector's buffer before being read
	More        bool
}

// RuntimeSnapshot is a reading of the application's runtime/metrics.
type RuntimeSnapshot struct {
	Now        int64                `json:"now"`
	Uint       map[string]uint64    `json:"uint"`
	Float      map[string]float64   `json:"float"`
	Histograms map[string]Histogram `json:"histograms"`
}

// Histogram is a runtime/metrics histogram.
type Histogram struct {
	Counts  []uint64  `json:"counts"`
	Buckets []float64 `json:"buckets"`
}

// Collector is a connection to the collector compiled into an application.
type Collector struct {
	PID     int
	Addr    string // host:port
	Mounted bool   // served by the application's own listener
	base    string
	token   string
	hc      *http.Client
	lease   string // from Arm: required to disarm and change rates
}

// Discovery is the result of looking for a collector.
type Discovery struct {
	Collector *Collector
	Info      CollectorInfo
	Notes     []string
}

// AnnounceDirs are the directories where collectors announce themselves
// (they mirror collector.AnnounceDir).
func AnnounceDirs() []string {
	var out []string
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() && ownedPrivate(fi) {
			out = append(out, filepath.Join(d, "gitview", "collectors"))
		}
	}
	if d, err := os.UserCacheDir(); err == nil {
		out = append(out, filepath.Join(d, "gitview", "collectors"))
	}
	return out
}

func newClient() *http.Client { return LoopbackClient(2 * time.Second) }

// validPath accepts the endpoint path of an announcement: an absolute path
// of letters, digits and / _ . - ending in "/" (nothing that could change
// the URL's host, like "@" or "//").
func validPath(p string) bool {
	if len(p) < 2 || p[0] != '/' || p[len(p)-1] != '/' || strings.Contains(p, "//") || strings.Contains(p, "..") {
		return false
	}
	for _, r := range p {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '/' || r == '_' || r == '.' || r == '-') {
			return false
		}
	}
	return true
}

// readAnnouncement reads an announcement only if it is a regular file of
// the current user that nobody else can read or write (a file planted by
// another user, or a symbolic link, is refused).
func readAnnouncement(path string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || !ownedPrivate(fi) {
		return nil, fmt.Errorf("%s is not a private file of the current user", path)
	}
	dir, err := os.Lstat(filepath.Dir(path))
	if err != nil || !dir.IsDir() || !ownedPrivate(dir) {
		return nil, fmt.Errorf("%s is not a private directory of the current user", filepath.Dir(path))
	}
	return os.ReadFile(path)
}

// FindCollector looks for the collector of process p: its announcement
// (written by the collector, readable only by the same user) gives the
// address and the token; the address must be a port owned by p.
func FindCollector(ctx context.Context, p *proc.Process) Discovery {
	var d Discovery
	var a *announcement
	for _, dir := range AnnounceDirs() {
		data, err := readAnnouncement(filepath.Join(dir, strconv.Itoa(p.PID)+".json"))
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				d.Notes = append(d.Notes, "collector announcement ignored: "+err.Error())
			}
			continue
		}
		var x announcement
		if json.Unmarshal(data, &x) != nil || x.Collector != "gitview" || x.PID != p.PID {
			continue
		}
		if !validPath(x.Path) {
			d.Notes = append(d.Notes, fmt.Sprintf("collector announcement with an invalid endpoint path %q ignored", x.Path))
			continue
		}
		// An announcement older than the process belongs to a previous
		// process that had the same PID.
		if !p.Started.IsZero() && time.Unix(0, x.Started).Before(p.Started.Add(-2*time.Second)) {
			d.Notes = append(d.Notes, "stale collector announcement (from an earlier process with the same PID) ignored")
			continue
		}
		a = &x
		break
	}
	if a == nil {
		d.Notes = append(d.Notes, "no GitView collector announced by this process (the collector is optional: see the collector module)")
		return d
	}
	if p.OwnNetNS {
		d.Notes = append(d.Notes, "the process runs in its own network namespace (a container): its ports cannot be reached at 127.0.0.1 from here, so its collector is not used")
		return d
	}
	var candidates []string
	if a.Addr != "" {
		host, port, err := net.SplitHostPort(a.Addr)
		n, _ := strconv.Atoi(port)
		if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() || !ownsPort(p, n) {
			d.Notes = append(d.Notes, fmt.Sprintf("the announced collector address %s is not a port of process %d; ignored", a.Addr, p.PID))
			return d
		}
		candidates = append(candidates, a.Addr)
	} else {
		for _, pt := range p.Ports {
			candidates = append(candidates, pt.Dial())
		}
	}
	for _, addr := range candidates {
		c := &Collector{PID: p.PID, Addr: addr, Mounted: a.Addr == "", base: "http://" + addr + a.Path, token: a.Token, hc: newClient()}
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		info, err := c.Info(cctx)
		cancel()
		if err != nil {
			if a.Addr != "" {
				d.Notes = append(d.Notes, "collector at "+addr+" did not answer: "+err.Error())
			}
			continue
		}
		if info.PID != p.PID {
			d.Notes = append(d.Notes, fmt.Sprintf("collector at %s reports pid %d (another PID namespace?); ignored", addr, info.PID))
			continue
		}
		d.Collector, d.Info = c, info
		return d
	}
	if a.Addr == "" {
		d.Notes = append(d.Notes, "the collector is mounted on the application's mux but none of its ports answered")
	}
	return d
}

func ownsPort(p *proc.Process, port int) bool {
	for _, pt := range p.Ports {
		if pt.Port == port {
			return true
		}
	}
	return false
}

func (c *Collector) do(ctx context.Context, method, path string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("the collector's reply exceeds %d MiB", max>>20)
	}
	if resp.StatusCode != http.StatusOK {
		msg := string(data)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, &StatusError{Code: resp.StatusCode, Msg: msg}
	}
	return data, nil
}

// StatusError is a non-200 reply of the collector.
type StatusError struct {
	Code int
	Msg  string
}

func (e *StatusError) Error() string { return fmt.Sprintf("collector: HTTP %d: %s", e.Code, e.Msg) }

func (c *Collector) getJSON(ctx context.Context, method, path string, v any) error {
	data, err := c.do(ctx, method, path, 64<<20)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// Info reads the collector's state.
func (c *Collector) Info(ctx context.Context) (CollectorInfo, error) {
	var in CollectorInfo
	err := c.getJSON(ctx, "GET", "info", &in)
	if err == nil && in.Collector != "gitview" {
		err = errors.New("not a GitView collector")
	}
	return in, err
}

// ArmOptions selects what the collector does while armed.
type ArmOptions struct {
	TTL     time.Duration // the collector disarms itself after it
	Labels  bool          // pprof labels with the trace ID (CPU attribution)
	Tasks   bool          // runtime/trace tasks per request
	Runtime bool          // GC cycles during each request
}

// Arm starts recording. The collector refuses when another GitView
// capture holds it.
func (c *Collector) Arm(ctx context.Context, o ArmOptions) (CollectorInfo, error) {
	b := func(v bool) string {
		if v {
			return "1"
		}
		return "0"
	}
	ttl := int(o.TTL / time.Second)
	if ttl <= 0 {
		ttl = 600
	}
	var in CollectorInfo
	err := c.getJSON(ctx, "POST", fmt.Sprintf("arm?ttl=%d&labels=%s&tasks=%s&runtime=%s", ttl, b(o.Labels), b(o.Tasks), b(o.Runtime)), &in)
	if err == nil {
		c.lease = in.Lease
	}
	return in, err
}

// Disarm stops recording and restores profiling rates.
func (c *Collector) Disarm(ctx context.Context) error {
	_, err := c.do(ctx, "POST", "disarm?lease="+url.QueryEscape(c.lease), 1<<20)
	return err
}

// Rates enables mutex (fraction) and block (nanoseconds) profiling until
// the capture is disarmed.
func (c *Collector) Rates(ctx context.Context, mutex, block int) error {
	_, err := c.do(ctx, "POST", fmt.Sprintf("profile/rates?mutex=%d&block=%d&lease=%s", mutex, block, url.QueryEscape(c.lease)), 1<<20)
	return err
}

// Runtime reads the application's runtime/metrics.
func (c *Collector) Runtime(ctx context.Context) (RuntimeSnapshot, error) {
	var s RuntimeSnapshot
	err := c.getJSON(ctx, "GET", "runtime", &s)
	return s, err
}

// Profile fetches a profile ("cpu" and "trace" last the given seconds),
// refusing replies larger than max bytes.
func (c *Collector) Profile(ctx context.Context, name string, seconds int, max int64) ([]byte, error) {
	path := "profile/" + name
	if seconds > 0 {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		path += sep + "seconds=" + strconv.Itoa(seconds)
	}
	return c.do(ctx, "GET", path, max)
}

// Requests reads at most max requests completed after since (and the
// statements outside requests after orphansSince).
func (c *Collector) Requests(ctx context.Context, since, orphansSince int64, max int) (Page, error) {
	var w wireRequests
	if err := c.getJSON(ctx, "GET", fmt.Sprintf("requests?since=%d&orphansSince=%d&max=%d", since, orphansSince, max), &w); err != nil {
		return Page{}, err
	}
	pg := Page{Last: w.Last, OrphansLast: w.OrphansLast, Lost: w.Lost, More: w.More}
	if n := len(w.Requests); n > 0 {
		pg.Last = w.Requests[n-1].Seq
	}
	for _, r := range w.Requests {
		req := Request{
			ID:            r.ID,
			W3C:           r.W3C,
			Sources:       []Source{SourceCollector},
			Method:        r.Method,
			Path:          MaskPath(r.Path),
			Route:         r.Route,
			Status:        r.Status,
			Start:         time.Unix(0, r.Start),
			HasServer:     true,
			ServerDur:     time.Duration(r.Dur),
			BytesIn:       r.BytesIn,
			BytesOut:      r.BytesOut,
			ContentLength: r.ContentLength,
			InFlight:      r.InFlight,
			Panic:         r.Panic,
			Labelled:      r.Labelled,
			SQLDropped:    r.SQLDropped,
		}
		if r.Runtime != nil {
			req.HasGC, req.GCDuring = true, r.Runtime.GCCycles
		}
		for _, s := range r.SQL {
			req.SQL = append(req.SQL, s.toSpan())
		}
		req.SetKey()
		pg.Requests = append(pg.Requests, req)
	}
	for _, s := range w.Orphans {
		sp := s.toSpan()
		sp.Start = 0
		pg.Orphans = append(pg.Orphans, OrphanSpan{Start: time.Unix(0, s.Start), SQLSpan: sp})
	}
	return pg, nil
}

func (s wireSpan) toSpan() SQLSpan {
	return SQLSpan{
		// Redacted again: the collector may be an older version.
		Query: RedactSQL(s.Query), Start: time.Duration(s.Start), Dur: time.Duration(s.Dur),
		File: s.File, Line: s.Line, Func: s.Func, Rows: s.Rows, HasRows: s.HasRows,
		Err: s.Err, Unfinished: s.Unfinished, Source: SourceCollector,
	}
}
