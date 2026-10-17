package collector

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Announcement is the file through which GitView finds a collector and
// its access token. It is readable only by the user running the process.
type Announcement struct {
	Collector string `json:"collector"` // "gitview"
	Version   int    `json:"version"`
	PID       int    `json:"pid"`
	Addr      string `json:"addr,omitempty"` // listener started by Start; empty when mounted on the application's mux
	Path      string `json:"path"`
	Token     string `json:"token"`
	Started   int64  `json:"started"`
}

// AnnounceDir is where announcements are written: $XDG_RUNTIME_DIR/gitview/collectors
// when XDG_RUNTIME_DIR is a private directory of the current user, the user
// cache directory otherwise.
func AnnounceDir() (string, error) {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() && ownedPrivate(fi) {
			return filepath.Join(d, "gitview", "collectors"), nil
		}
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "gitview", "collectors"), nil
}

// privateDir creates dir (0700) and checks that it is a directory of the
// current user that nobody else can use.
func privateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() || !ownedBySelf(fi) {
		return fmt.Errorf("%s is not a directory of the current user", dir)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// announce writes the announcement: created exclusively with mode 0600 (a
// file or symlink planted at a predictable name is never written through),
// then renamed into place.
func (c *Collector) announce(addr string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	dir, err := AnnounceDir()
	if err != nil {
		return err
	}
	if err := privateDir(dir); err != nil {
		return err
	}
	data, _ := json.Marshal(Announcement{Collector: "gitview", Version: Version, PID: os.Getpid(), Addr: addr, Path: BasePath, Token: c.token, Started: c.started.UnixNano()})
	f, err := os.CreateTemp(dir, ".announce-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	path := filepath.Join(dir, strconv.Itoa(os.Getpid())+".json")
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	c.announced = path
	return nil
}

var errDisabled = errors.New("the GitView collector is disabled (" + EnvDisable + "=off)")

// Start serves the collector's endpoints on a loopback address (default
// 127.0.0.1 with a random port) and announces it to GitView. It returns
// the address listened on.
func (c *Collector) Start(addr string) (string, error) {
	if c.disabled {
		return "", errDisabled
	}
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", fmt.Errorf("the collector only listens on loopback addresses, not %q", host)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	srv := &http.Server{Handler: c.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(ln)
	c.mu.Lock()
	c.closers = append(c.closers, srv.Close)
	c.mu.Unlock()
	if err := c.announce(ln.Addr().String()); err != nil {
		srv.Close()
		return "", fmt.Errorf("announcing the collector: %w", err)
	}
	return ln.Addr().String(), nil
}

// Close stops the listener started by Start, disarms the collector and
// removes the announcement.
func (c *Collector) Close() error {
	c.disarm()
	c.mu.Lock()
	closers, path := c.closers, c.announced
	c.closers, c.announced = nil, ""
	c.mu.Unlock()
	for _, f := range closers {
		f()
	}
	if path != "" {
		os.Remove(path)
	}
	return nil
}

// Handler serves the collector's endpoints under BasePath. Prefer Start,
// which keeps them on a separate loopback listener: when the handler is
// mounted on the application's mux, a reverse proxy in front of the
// application could reach it (requests carrying forwarding headers are
// refused, and every request needs the token).
func (c *Collector) Handler() http.Handler {
	if c.disabled {
		return http.NotFoundHandler()
	}
	c.handlerOnce.Do(func() {
		c.mu.Lock()
		done := c.announced != ""
		c.mu.Unlock()
		if !done {
			c.announce("")
		}
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+BasePath+"info", c.handleInfo)
	mux.HandleFunc("POST "+BasePath+"arm", c.handleArm)
	mux.HandleFunc("POST "+BasePath+"disarm", c.handleDisarm)
	mux.HandleFunc("GET "+BasePath+"requests", c.handleRequests)
	mux.HandleFunc("GET "+BasePath+"runtime", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, readRuntime()) })
	mux.HandleFunc("GET "+BasePath+"profile/{name}", c.handleProfile)
	mux.HandleFunc("POST "+BasePath+"profile/rates", c.handleRates)
	return c.guard(mux)
}

func (c *Collector) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			http.Error(w, "the GitView collector only answers loopback clients", http.StatusForbidden)
			return
		}
		for _, h := range []string{"Forwarded", "X-Forwarded-For", "X-Real-Ip"} {
			if r.Header.Get(h) != "" {
				http.Error(w, "the GitView collector refuses forwarded requests", http.StatusForbidden)
				return
			}
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(c.token)) != 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"collector":"gitview","version":%d,"error":"token required"}`, Version)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// Info is the reply of the info endpoint.
type Info struct {
	Collector string `json:"collector"`
	Version   int    `json:"version"`
	PID       int    `json:"pid"`
	GoVersion string `json:"goVersion"`
	Started   int64  `json:"started"`
	Capacity  int    `json:"capacity"`
	Armed     bool   `json:"armed"`
	Until     int64  `json:"until,omitempty"`
	Last      int64  `json:"last"`
	InFlight  int64  `json:"inFlight"`
	Lease     string `json:"lease,omitempty"` // only in the reply to arm
}

func (c *Collector) info() Info {
	in := Info{Collector: "gitview", Version: Version, PID: os.Getpid(), GoVersion: runtime.Version(), Started: c.started.UnixNano(), Capacity: c.opts.Capacity, InFlight: c.inFlight.Load()}
	if st := c.state(); st != nil {
		in.Armed, in.Until = true, st.until.UnixNano()
	}
	c.mu.Lock()
	in.Last = c.next
	c.mu.Unlock()
	return in
}

func (c *Collector) handleInfo(w http.ResponseWriter, r *http.Request) { writeJSON(w, c.info()) }

func boolParam(r *http.Request, name string, def bool) bool {
	switch r.URL.Query().Get(name) {
	case "0", "false", "off":
		return false
	case "1", "true", "on":
		return true
	}
	return def
}

func intParam(r *http.Request, name string, def, min, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// handleArm starts recording for ttl seconds (default 600, at most 3600):
// an abandoned capture stops by itself. The reply carries a lease that
// disarming and changing rates require, so that two GitView sessions do not
// stop each other; arming while another lease is active is refused.
func (c *Collector) handleArm(w http.ResponseWriter, r *http.Request) {
	ttl := time.Duration(intParam(r, "ttl", 600, 1, 3600)) * time.Second
	if cur := c.state(); cur != nil && cur.lease != r.URL.Query().Get("lease") {
		http.Error(w, "the collector is armed by another GitView capture until "+cur.until.Format(time.RFC3339), http.StatusConflict)
		return
	}
	st := &armState{
		until:   time.Now().Add(ttl),
		labels:  boolParam(r, "labels", true),
		tasks:   boolParam(r, "tasks", true),
		runtime: boolParam(r, "runtime", true),
		lease:   NewID(),
	}
	c.armed.Store(st)
	c.mu.Lock()
	if c.rates.timer != nil {
		c.rates.timer.Stop()
	}
	c.rates.timer = time.AfterFunc(ttl, func() {
		if c.armed.Load() == st {
			c.disarm()
		}
	})
	c.mu.Unlock()
	in := c.info()
	in.Lease = st.lease
	writeJSON(w, in)
}

// leaseOK reports whether the request holds the current lease (or nothing
// is armed).
func (c *Collector) leaseOK(r *http.Request) bool {
	cur := c.state()
	return cur == nil || cur.lease == r.URL.Query().Get("lease")
}

func (c *Collector) handleDisarm(w http.ResponseWriter, r *http.Request) {
	if !c.leaseOK(r) {
		http.Error(w, "the collector is armed by another GitView capture", http.StatusConflict)
		return
	}
	c.disarm()
	writeJSON(w, c.info())
}

// disarm stops recording and restores the profiling rates changed for the
// capture.
func (c *Collector) disarm() {
	c.armed.Store(nil)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rates.timer != nil {
		c.rates.timer.Stop()
		c.rates.timer = nil
	}
	if c.rates.mutexSet {
		runtime.SetMutexProfileFraction(c.rates.mutexPrev)
		c.rates.mutexSet = false
	}
	if c.rates.blockSet {
		// The runtime cannot report the previous rate: restore the one the
		// application set through SetBlockProfileRate (0 otherwise).
		runtime.SetBlockProfileRate(c.rates.blockApp)
		c.rates.blockSet = false
	}
}

// SetBlockProfileRate sets runtime.SetBlockProfileRate and remembers the
// value, so that it is restored after a capture that enabled block
// profiling. Applications that profile blocking themselves should call this
// instead of the runtime function.
func (c *Collector) SetBlockProfileRate(rate int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rates.blockApp = rate
	if !c.rates.blockSet {
		runtime.SetBlockProfileRate(rate)
	}
}

type rates struct {
	timer     *time.Timer
	mutexSet  bool
	mutexPrev int
	blockSet  bool
	blockApp  int
}

// RequestsReply is the reply of the requests endpoint.
type RequestsReply struct {
	PID      int       `json:"pid"`
	Now      int64     `json:"now"`
	Last     int64     `json:"last"` // sequence of the newest completed request
	Lost     int64     `json:"lost"` // requests dropped from the ring before they were read
	More     bool      `json:"more"`
	Requests []Request `json:"requests"`
	// Statements executed outside any recorded request.
	Orphans     []SQLSpan `json:"orphans,omitempty"`
	OrphansLast int64     `json:"orphansLast"`
}

func (c *Collector) handleRequests(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	max := intParam(r, "max", 1000, 1, 5000)
	reqs, lost, last := c.since(since, max)
	rep := RequestsReply{PID: os.Getpid(), Now: time.Now().UnixNano(), Last: last, Lost: lost, Requests: reqs}
	if len(reqs) > 0 && reqs[len(reqs)-1].Seq < last {
		rep.More = true
	}
	if rep.Requests == nil {
		rep.Requests = []Request{}
	}
	osince, _ := strconv.ParseInt(r.URL.Query().Get("orphansSince"), 10, 64)
	c.mu.Lock()
	rep.OrphansLast = c.orphSeq
	first := c.orphSeq - int64(len(c.orphans)) + 1
	for s := max64(osince+1, first); s <= c.orphSeq; s++ {
		rep.Orphans = append(rep.Orphans, c.orphans[int((s-1)%maxOrphans)])
	}
	c.mu.Unlock()
	writeJSON(w, rep)
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

var (
	cpuMu   sync.Mutex
	traceMu sync.Mutex
)

// handleProfile serves runtime/pprof profiles and runtime/trace traces.
// heap and allocs accept ?gc=1 to run a garbage collection first.
// The CPU profile and the trace last ?seconds= (CPU: 1–60, default 10;
// trace: 1–30, default 5) and end early when GitView cancels the request.
func (c *Collector) handleProfile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	switch name {
	case "cpu":
		if !cpuMu.TryLock() {
			http.Error(w, "a CPU profile is already being collected by the GitView collector", http.StatusConflict)
			return
		}
		defer cpuMu.Unlock()
		secs := intParam(r, "seconds", 10, 1, 60)
		w.Header().Set("Content-Type", "application/octet-stream")
		if err := pprof.StartCPUProfile(w); err != nil {
			w.Header().Del("Content-Type")
			http.Error(w, "CPU profiling is already in use in this process: "+err.Error(), http.StatusConflict)
			return
		}
		sleep(r, time.Duration(secs)*time.Second)
		pprof.StopCPUProfile()
	case "trace":
		if !traceMu.TryLock() {
			http.Error(w, "a trace is already being collected by the GitView collector", http.StatusConflict)
			return
		}
		defer traceMu.Unlock()
		secs := intParam(r, "seconds", 5, 1, 30)
		w.Header().Set("Content-Type", "application/octet-stream")
		if err := trace.Start(w); err != nil {
			w.Header().Del("Content-Type")
			http.Error(w, "execution tracing is already in use in this process: "+err.Error(), http.StatusConflict)
			return
		}
		sleep(r, time.Duration(secs)*time.Second)
		trace.Stop()
	case "heap", "allocs", "goroutine", "mutex", "block", "threadcreate":
		p := pprof.Lookup(name)
		if p == nil {
			http.NotFound(w, r)
			return
		}
		if (name == "heap" || name == "allocs") && r.URL.Query().Get("gc") == "1" {
			// Requested so that the allocation counts are current (the heap
			// profile is otherwise as of the last completed GC).
			runtime.GC()
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		p.WriteTo(w, 0)
	default:
		http.NotFound(w, r)
	}
}

func sleep(r *http.Request, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-r.Context().Done():
	}
}

// handleRates enables mutex and block profiling while a capture is armed
// (?mutex=fraction, 1–1000; ?block=nanoseconds, 1–1e9). They are restored
// when the capture is disarmed or expires.
func (c *Collector) handleRates(w http.ResponseWriter, r *http.Request) {
	if c.state() == nil || !c.leaseOK(r) {
		http.Error(w, "profiling rates can only be changed by the capture that armed the collector", http.StatusConflict)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if m := intParam(r, "mutex", 0, 0, 1000); m > 0 {
		prev := runtime.SetMutexProfileFraction(m)
		if !c.rates.mutexSet {
			c.rates.mutexPrev, c.rates.mutexSet = prev, true
		}
	}
	if b := intParam(r, "block", 0, 0, 1_000_000_000); b > 0 {
		runtime.SetBlockProfileRate(b)
		c.rates.blockSet = true
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"mutex":%v,"block":%v}`, c.rates.mutexSet, c.rates.blockSet)
}
