// Package httpcol is the HTTP Collector of the Runtime Performance
// Inspector. It observes the requests a Go server handles during a capture
// through up to three sources, each optional:
//
//   - a local reverse proxy started by GitView on 127.0.0.1 (no change to
//     the application; the client sends its requests to the proxy);
//   - GitView's collector middleware compiled into the application
//     (github.com/athos/gitview/collector), read over its loopback endpoint;
//   - OpenTelemetry spans exported by the application to a local OTLP/HTTP
//     receiver.
//
// Requests seen by several sources are merged by their X-GitView-Trace-ID.
// No header, cookie, query string or body is ever recorded.
package httpcol

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// Source identifies who observed a request.
type Source string

const (
	SourceProxy     Source = "proxy"
	SourceCollector Source = "collector"
	SourceOTel      Source = "otel"
)

// HeaderTraceID is the request identifier header shared with the collector.
const HeaderTraceID = "X-GitView-Trace-ID"

// Request is one HTTP request observed during a capture. Durations from
// different sources measure different things: Proxy covers the client's
// view through the proxy (including the extra local hop and the client's
// reading of the response); Server covers the handler inside the process.
type Request struct {
	ID      string   `json:"id"`            // unique per request
	W3C     string   `json:"w3c,omitempty"` // W3C trace ID (a trace may hold several requests)
	Sources []Source `json:"sources"`
	Method  string   `json:"method"`
	Path    string   `json:"path"` // without query string; token-like segments masked
	// Route is the router's pattern when the application reported it
	// (collector or OpenTelemetry); otherwise empty.
	Route string `json:"route,omitempty"`
	// Key groups requests of the same endpoint: the method and the route
	// pattern, or the path with identifiers replaced by placeholders.
	Key      string `json:"key"`
	KeyBasis string `json:"keyBasis"` // "route pattern" | "normalized path"
	Status   int    `json:"status"`

	Start time.Time     `json:"start"` // wall clock
	Off   time.Duration `json:"off"`   // offset on the capture timeline

	HasProxy  bool          `json:"hasProxy"`
	ProxyDur  time.Duration `json:"proxyDur,omitempty"`
	HasTTFB   bool          `json:"hasTTFB,omitempty"`
	TTFB      time.Duration `json:"ttfb,omitempty"` // proxy: until the response headers
	HasServer bool          `json:"hasServer"`
	ServerDur time.Duration `json:"serverDur,omitempty"`

	BytesIn       int64 `json:"bytesIn"`
	BytesOut      int64 `json:"bytesOut"`
	ContentLength int64 `json:"contentLength"`

	InFlight   int64     `json:"inFlight,omitempty"` // as seen by the collector at the start
	Panic      bool      `json:"panic,omitempty"`
	Error      string    `json:"error,omitempty"` // proxy: upstream failure
	Labelled   bool      `json:"labelled,omitempty"`
	HasGC      bool      `json:"hasGC,omitempty"`
	GCDuring   uint64    `json:"gcDuring,omitempty"` // GC cycles that completed while it was served (process-wide)
	SQL        []SQLSpan `json:"sql,omitempty"`
	SQLDropped int       `json:"sqlDropped,omitempty"`
}

// Dur is the request's duration: the server-side one when known, else
// the proxy's.
func (r *Request) Dur() time.Duration {
	if r.HasServer {
		return r.ServerDur
	}
	return r.ProxyDur
}

// End is the offset of the end of the request on the capture timeline.
func (r *Request) End() time.Duration { return r.Off + r.Dur() }

// SQLSpan is one SQL statement executed while serving a request.
type SQLSpan struct {
	Query      string        `json:"query"` // literals removed
	Start      time.Duration `json:"start"` // from the start of the request
	Dur        time.Duration `json:"dur"`
	File       string        `json:"file,omitempty"`
	Line       int           `json:"line,omitempty"`
	Func       string        `json:"func,omitempty"`
	Rows       int64         `json:"rows,omitempty"`
	HasRows    bool          `json:"hasRows,omitempty"`
	Err        string        `json:"err,omitempty"`
	Source     Source        `json:"source"`
	Unfinished bool          `json:"unfinished,omitempty"`
}

// SetKey fills Key and KeyBasis.
func (r *Request) SetKey() {
	if r.Route != "" {
		route := r.Route
		// ServeMux patterns may already carry the method ("GET /x/{id}").
		if !strings.Contains(route, " ") {
			route = r.Method + " " + route
		}
		r.Key, r.KeyBasis = route, "route pattern"
		return
	}
	r.Key, r.KeyBasis = r.Method+" "+NormalizePath(r.Path), "normalized path"
}

// NormalizePath replaces path segments that look like identifiers with
// placeholders, so that /clients/42 and /clients/7 group together.
func NormalizePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		switch {
		case s == "" || strings.HasPrefix(s, "{"):
		case isDigits(s):
			parts[i] = "{n}"
		case isUUID(s):
			parts[i] = "{uuid}"
		case len(s) >= 16 && isHex(s):
			parts[i] = "{hex}"
		}
	}
	return strings.Join(parts, "/")
}

// Merge combines the records of the same request from several sources
// and returns them ordered by start. Records are joined by request ID (the
// proxy's ID travels to the collector, and to OpenTelemetry as the trace ID
// of the traceparent it adds). Two records of the same source are never
// joined. An OpenTelemetry request whose ID matches nothing is joined to the
// record with the same W3C trace ID, method and path whose time window
// contains it, when there is exactly one. The proxy provides the
// client-side timing; the collector and OpenTelemetry the route, the
// server-side timing and the SQL statements.
func Merge(lists ...[]Request) []Request {
	byID := map[string]*Request{}
	var all []*Request
	for _, list := range lists {
		for _, r := range list {
			if cur, ok := byID[r.ID]; ok && r.ID != "" {
				if !sharesSource(cur, r) {
					mergeInto(cur, r)
					continue
				}
				for n := 2; ; n++ {
					if _, taken := byID[r.ID+"#"+strconv.Itoa(n)]; !taken {
						r.ID += "#" + strconv.Itoa(n)
						break
					}
				}
			}
			c := r
			all = append(all, &c)
			if c.ID != "" {
				byID[c.ID] = &c
			}
		}
	}
	// OpenTelemetry records left alone: join by trace, method, path, time.
	merged := map[*Request]bool{}
	for _, r := range all {
		if len(r.Sources) != 1 || r.Sources[0] != SourceOTel || r.W3C == "" {
			continue
		}
		// A server span lies inside the client-side window; a few
		// milliseconds of slack cover clock granularity.
		var match *Request
		for _, slack := range []time.Duration{2 * time.Millisecond, 20 * time.Millisecond} {
			n := 0
			for _, c := range all {
				if c == r || merged[c] || hasSource(c.Sources, SourceOTel) || c.W3C != r.W3C || c.Method != r.Method ||
					NormalizePath(c.Path) != NormalizePath(r.Path) {
					continue
				}
				if r.Start.Before(c.Start.Add(-slack)) || r.Start.Add(r.Dur()).After(c.Start.Add(c.Dur()+slack)) {
					continue
				}
				match = c
				n++
			}
			if n == 1 {
				break
			}
			match = nil
		}
		n := 0
		if match != nil {
			n = 1
		}
		if n == 1 {
			mergeInto(match, *r)
			merged[r] = true
		}
	}
	out := make([]Request, 0, len(all))
	for _, r := range all {
		if merged[r] {
			continue
		}
		r.SetKey()
		out = append(out, *r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

func sharesSource(cur *Request, r Request) bool {
	for _, s := range r.Sources {
		if hasSource(cur.Sources, s) {
			return true
		}
	}
	return false
}

func mergeInto(cur *Request, r Request) {
	for _, s := range r.Sources {
		if !hasSource(cur.Sources, s) {
			cur.Sources = append(cur.Sources, s)
		}
	}
	if r.Route != "" && cur.Route == "" {
		cur.Route = r.Route
	}
	if cur.W3C == "" {
		cur.W3C = r.W3C
	}
	if cur.Method == "" {
		cur.Method = r.Method
	}
	if cur.Path == "" {
		cur.Path = r.Path
	}
	if r.HasProxy && !cur.HasProxy {
		cur.HasProxy, cur.ProxyDur, cur.HasTTFB, cur.TTFB = true, r.ProxyDur, r.HasTTFB, r.TTFB
		// The proxy saw the request first: its start is the client's.
		if !r.Start.IsZero() {
			cur.Start = r.Start
		}
		cur.Error = r.Error
	}
	if r.HasServer && !cur.HasServer {
		cur.HasServer, cur.ServerDur = true, r.ServerDur
		if cur.Start.IsZero() {
			cur.Start = r.Start
		}
	}
	if cur.Status == 0 || hasSource(r.Sources, SourceProxy) && r.Status != 0 {
		cur.Status = r.Status
	}
	if r.BytesIn > cur.BytesIn {
		cur.BytesIn = r.BytesIn
	}
	if r.BytesOut > cur.BytesOut {
		cur.BytesOut = r.BytesOut
	}
	if cur.ContentLength == 0 {
		cur.ContentLength = r.ContentLength
	}
	if r.InFlight > 0 {
		cur.InFlight = r.InFlight
	}
	cur.Panic = cur.Panic || r.Panic
	cur.Labelled = cur.Labelled || r.Labelled
	if r.HasGC {
		cur.HasGC, cur.GCDuring = true, r.GCDuring
	}
	// SQL from the collector is preferred to OpenTelemetry's (it carries
	// the call site); keep both when only one source has statements.
	if len(cur.SQL) == 0 {
		cur.SQL, cur.SQLDropped = r.SQL, r.SQLDropped
	}
}

func hasSource(list []Source, s Source) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
