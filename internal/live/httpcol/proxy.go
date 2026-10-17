package httpcol

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// ProxyOptions configures the local proxy.
type ProxyOptions struct {
	// Listen is the proxy's address (default 127.0.0.1:0, a free port).
	// Only loopback addresses are accepted.
	Listen string
	// Target is the application's address (host:port, loopback only).
	Target string
	// Traceparent adds a W3C traceparent header carrying the request ID
	// when the client sent none, so that OpenTelemetry spans of the
	// application share the ID.
	Traceparent bool
	// Sink receives each completed request; it is called concurrently.
	Sink func(Request)
}

// Proxy is GitView's local reverse proxy: the client (Postman, curl…)
// sends its requests to the proxy, which forwards them to the application
// unchanged except for the X-GitView-Trace-ID header, and records method,
// path, status, timing and sizes. Headers and bodies are never recorded.
type Proxy struct {
	opts     ProxyOptions
	ln       net.Listener
	srv      *http.Server
	rp       *httputil.ReverseProxy
	inFlight atomic.Int64
	count    atomic.Int64
}

// IsLoopbackHost reports whether host (an IP or "localhost") is loopback.
func IsLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func checkLoopback(what, addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s %q: %w", what, addr, err)
	}
	if !IsLoopbackHost(host) {
		return fmt.Errorf("%s must be a loopback address (127.0.0.1, ::1 or localhost), not %q", what, host)
	}
	return nil
}

type proxyCtx struct {
	id  string
	err error
}

type proxyKey struct{}

// StartProxy starts the proxy.
func StartProxy(opts ProxyOptions) (*Proxy, error) {
	if opts.Listen == "" {
		opts.Listen = "127.0.0.1:0"
	}
	if err := checkLoopback("the proxy address", opts.Listen); err != nil {
		return nil, err
	}
	if err := checkLoopback("the application address", opts.Target); err != nil {
		return nil, err
	}
	p := &Proxy{opts: opts}
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		MaxIdleConnsPerHost: 64,
		IdleConnTimeout:     30 * time.Second,
		DisableCompression:  true, // pass Accept-Encoding through untouched
	}
	target := &url.URL{Scheme: "http", Host: opts.Target}
	p.rp = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pc := pr.In.Context().Value(proxyKey{}).(*proxyCtx)
			pr.SetURL(target)
			pr.Out.Header.Set(HeaderTraceID, pc.id)
			if opts.Traceparent && pr.Out.Header.Get("traceparent") == "" {
				pr.Out.Header.Set("traceparent", "00-"+pc.id+"-"+NewID()[:16]+"-01")
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			pc := resp.Request.Context().Value(proxyKey{}).(*proxyCtx)
			resp.Header.Set(HeaderTraceID, pc.id)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			pc := r.Context().Value(proxyKey{}).(*proxyCtx)
			pc.err = err
			if errors.Is(err, context.Canceled) {
				return // the client went away
			}
			w.Header().Set(HeaderTraceID, pc.id)
			http.Error(w, "GitView proxy: the application did not answer: "+err.Error(), http.StatusBadGateway)
		},
		Transport: transport,
	}
	ln, err := net.Listen("tcp", opts.Listen)
	if err != nil {
		return nil, err
	}
	p.ln = ln
	p.srv = &http.Server{Handler: p, ReadHeaderTimeout: 30 * time.Second}
	go p.srv.Serve(ln)
	return p, nil
}

// Addr is the address the client should send its requests to.
func (p *Proxy) Addr() string { return p.ln.Addr().String() }

// Target is the application's address.
func (p *Proxy) Target() string { return p.opts.Target }

// Count is the number of requests proxied so far.
func (p *Proxy) Count() int64 { return p.count.Load() }

// Close stops accepting requests and waits up to two seconds for those in
// progress.
func (p *Proxy) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.srv.Shutdown(ctx); err != nil {
		return p.srv.Close()
	}
	return nil
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	// Every request gets its own ID: a header repeated by the client must
	// not merge requests. A client's traceparent identifies a trace (it may
	// cover many requests), kept apart as W3C.
	id := NewID()
	w3c := ""
	if tp := r.Header.Get("traceparent"); len(tp) >= 55 && tp[2] == '-' && ValidID(tp[3:35]) {
		w3c = strings.ToLower(tp[3:35])
	}
	inFlight := p.inFlight.Add(1)
	defer p.inFlight.Add(-1)
	pc := &proxyCtx{id: id}
	r = r.WithContext(context.WithValue(r.Context(), proxyKey{}, pc))
	var body *countingReader
	if r.Body != nil && r.Body != http.NoBody {
		body = &countingReader{rc: r.Body}
		r.Body = body
	}
	pw := &proxyWriter{w: w, start: start}
	p.rp.ServeHTTP(pw, r)

	if w3c == "" && p.opts.Traceparent {
		w3c = id // sent to the application as its trace ID
	}
	req := Request{
		ID:            id,
		W3C:           w3c,
		Sources:       []Source{SourceProxy},
		Method:        r.Method,
		Path:          MaskPath(r.URL.Path),
		Status:        pw.status,
		Start:         start,
		HasProxy:      true,
		ProxyDur:      time.Since(start),
		BytesOut:      pw.n,
		ContentLength: r.ContentLength,
		InFlight:      inFlight,
	}
	if pw.ttfb > 0 {
		req.HasTTFB, req.TTFB = true, pw.ttfb
	}
	if body != nil {
		req.BytesIn = body.n.Load()
	}
	if pc.err != nil {
		req.Error = pc.err.Error()
		if req.Status == 0 {
			req.Status = 499 // the client closed the request
		}
	}
	if req.Status == 0 {
		req.Status = http.StatusOK
	}
	req.SetKey()
	p.count.Add(1)
	if p.opts.Sink != nil {
		p.opts.Sink(req)
	}
}

type countingReader struct {
	rc io.ReadCloser
	n  atomic.Int64
}

func (c *countingReader) Read(b []byte) (int, error) {
	n, err := c.rc.Read(b)
	c.n.Add(int64(n))
	return n, err
}

func (c *countingReader) Close() error { return c.rc.Close() }

// proxyWriter records the status, the size of the response and the time
// its headers were sent.
type proxyWriter struct {
	w      http.ResponseWriter
	start  time.Time
	status int
	n      int64
	ttfb   time.Duration
}

func (w *proxyWriter) Header() http.Header { return w.w.Header() }

func (w *proxyWriter) WriteHeader(code int) {
	if w.status == 0 && code >= 200 {
		w.status = code
		w.ttfb = time.Since(w.start)
	}
	w.w.WriteHeader(code)
}

func (w *proxyWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.w.Write(b)
	w.n += int64(n)
	return n, err
}

func (w *proxyWriter) Flush() {
	if f, ok := w.w.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *proxyWriter) Unwrap() http.ResponseWriter { return w.w }
