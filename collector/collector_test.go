package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newTest(t *testing.T, opts Options) *Collector {
	t.Helper()
	privateRuntimeDir(t)
	t.Setenv(EnvDisable, "")
	c := New(opts)
	t.Cleanup(func() { c.Close() })
	return c
}

// call performs an authorised request to one of the collector's endpoints.
func call(t *testing.T, c *Collector, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "127.0.0.1:5000"
	req.Header.Set("Authorization", "Bearer "+c.token)
	rec := httptest.NewRecorder()
	c.Handler().ServeHTTP(rec, req)
	return rec
}

// arm arms the collector and returns the lease.
func arm(t *testing.T, c *Collector) string {
	t.Helper()
	rec := call(t, c, "POST", BasePath+"arm?ttl=60")
	if rec.Code != 200 {
		t.Fatalf("arm: %d %s", rec.Code, rec.Body)
	}
	var in Info
	json.Unmarshal(rec.Body.Bytes(), &in)
	if in.Lease == "" {
		t.Fatal("no lease")
	}
	return in.Lease
}

func requests(t *testing.T, c *Collector, since int) RequestsReply {
	t.Helper()
	rec := call(t, c, "GET", BasePath+"requests?since="+strconv.Itoa(since))
	var rep RequestsReply
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatalf("requests: %v %s", err, rec.Body)
	}
	return rep
}

func app(c *Collector) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /clients/{id}", func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		span := c.StartSQL(r.Context(), "SELECT id, name FROM clients WHERE id = $1 AND token = 'hunter2' -- secret")
		time.Sleep(2 * time.Millisecond)
		span.EndRows(nil, 1)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("GET /boom", func(w http.ResponseWriter, r *http.Request) { panic("boom") })
	return c.Middleware(mux)
}

func TestIdleCollectorRecordsNothing(t *testing.T) {
	c := newTest(t, Options{})
	rec := httptest.NewRecorder()
	app(c).ServeHTTP(rec, httptest.NewRequest("POST", "/clients/1", strings.NewReader("x")))
	if rec.Code != 201 || rec.Header().Get(HeaderTraceID) != "" {
		t.Errorf("idle: %d %q", rec.Code, rec.Header().Get(HeaderTraceID))
	}
	if rep := requests(t, c, 0); len(rep.Requests) != 0 || rep.Last != 0 {
		t.Errorf("idle collector recorded %+v", rep)
	}
}

func TestArmedCollectorRecordsRequests(t *testing.T) {
	c := newTest(t, Options{})
	arm(t, c)
	h := app(c)
	req := httptest.NewRequest("POST", "/clients/42?api_key=s3cr3t", strings.NewReader(`{"name":"x"}`))
	req.Header.Set(HeaderTraceID, "trace-0001")
	req.Header.Set("Cookie", "session=abcdef")
	req.Header.Set("Authorization", "Bearer topsecret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 201 || rec.Header().Get(HeaderTraceID) != "trace-0001" {
		t.Fatalf("response: %d %q", rec.Code, rec.Header().Get(HeaderTraceID))
	}
	// A W3C traceparent is adopted; without either a new ID is generated.
	req = httptest.NewRequest("POST", "/clients/7", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	h.ServeHTTP(httptest.NewRecorder(), req)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/clients/8", nil))

	rep := requests(t, c, 0)
	if len(rep.Requests) != 3 || rep.Last != 3 || rep.Lost != 0 {
		t.Fatalf("requests: %+v", rep)
	}
	r := rep.Requests[0]
	if r.ID != "trace-0001" || r.Method != "POST" || r.Path != "/clients/{n}" || r.Route != "POST /clients/{id}" || r.Status != 201 {
		t.Errorf("request: %+v", r)
	}
	if r.BytesIn != 12 || r.ContentLength != 12 || r.BytesOut != 11 || r.InFlight != 1 || !r.Labelled || r.Runtime == nil {
		t.Errorf("sizes/flags: %+v", r)
	}
	if r.Dur < int64(2*time.Millisecond) || r.Start == 0 {
		t.Errorf("timing: %+v", r)
	}
	if len(r.SQL) != 1 {
		t.Fatalf("sql: %+v", r.SQL)
	}
	s := r.SQL[0]
	if s.Query != "SELECT id, name FROM clients WHERE id = $1 AND token = ?" || !s.HasRows || s.Rows != 1 || s.Dur < int64(2*time.Millisecond) {
		t.Errorf("span: %+v", s)
	}
	if filepath.Base(s.File) != "collector_test.go" || !strings.HasSuffix(s.Func, "collector.app.func1") {
		t.Errorf("call site: %s:%d %s", s.File, s.Line, s.Func)
	}
	// A traceparent identifies the trace, not the request.
	if r1 := rep.Requests[1]; r1.W3C != "4bf92f3577b34da6a3ce929d0e0e4736" || len(r1.ID) != 32 || r1.ID == r1.W3C || len(rep.Requests[2].ID) != 32 {
		t.Errorf("ids: %+v / %q", r1, rep.Requests[2].ID)
	}
	raw := call(t, c, "GET", BasePath+"requests").Body.String()
	for _, secret := range []string{"s3cr3t", "abcdef", "topsecret", "hunter2", "secret"} {
		if strings.Contains(raw, secret) {
			t.Errorf("%q leaked: %s", secret, raw)
		}
	}
	// Incremental reads.
	if rep := requests(t, c, 2); len(rep.Requests) != 1 || rep.Requests[0].Seq != 3 {
		t.Errorf("since 2: %+v", rep.Requests)
	}
}

func TestRepeatedIDsDoNotMergeRequests(t *testing.T) {
	c := newTest(t, Options{})
	arm(t, c)
	h := app(c)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("POST", "/clients/1", nil)
		req.Header.Set(HeaderTraceID, "same-id-every-time")
		req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	rep := requests(t, c, 0)
	ids := map[string]bool{}
	for _, r := range rep.Requests {
		ids[r.ID] = true
	}
	if len(rep.Requests) != 3 || len(ids) != 3 || !ids["same-id-every-time"] {
		t.Errorf("requests %d, distinct ids %v", len(rep.Requests), ids)
	}
}

func TestPanicIsRecordedAndPropagated(t *testing.T) {
	c := newTest(t, Options{})
	arm(t, c)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic swallowed")
			}
		}()
		app(c).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/boom", nil))
	}()
	rep := requests(t, c, 0)
	if len(rep.Requests) != 1 || !rep.Requests[0].Panic || rep.Requests[0].Status != 500 {
		t.Errorf("panic: %+v", rep.Requests)
	}
	if c.inFlight.Load() != 0 {
		t.Error("in-flight counter leaked")
	}
}

func TestRingOverflowIsReported(t *testing.T) {
	c := newTest(t, Options{Capacity: 3})
	arm(t, c)
	h := app(c)
	for i := 0; i < 5; i++ {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/clients/1", nil))
	}
	rep := requests(t, c, 0)
	if rep.Lost != 2 || len(rep.Requests) != 3 || rep.Requests[0].Seq != 3 || rep.Requests[2].Seq != 5 {
		t.Errorf("overflow: lost=%d %d requests", rep.Lost, len(rep.Requests))
	}
}

func TestOrphanStatements(t *testing.T) {
	c := newTest(t, Options{})
	if s := c.StartSQL(context.Background(), "SELECT 1"); s != nil {
		t.Error("statement recorded while disarmed")
	}
	arm(t, c)
	for i := 0; i < maxOrphans+2; i++ {
		c.StartSQL(context.Background(), "DELETE FROM jobs WHERE id = 99").End(errors.New("x"))
	}
	rec := call(t, c, "GET", BasePath+"requests?orphansSince="+strconv.Itoa(maxOrphans))
	var rep RequestsReply
	json.Unmarshal(rec.Body.Bytes(), &rep)
	if rep.OrphansLast != maxOrphans+2 || len(rep.Orphans) != 2 || rep.Orphans[0].Query != "DELETE FROM jobs WHERE id = ?" || rep.Orphans[0].Err != "*errors.errorString" {
		t.Errorf("orphans: last=%d %+v", rep.OrphansLast, rep.Orphans)
	}
}

func TestGuard(t *testing.T) {
	c := newTest(t, Options{})
	h := c.Handler()
	try := func(remote, token string, hdr ...string) int {
		req := httptest.NewRequest("GET", BasePath+"info", nil)
		req.RemoteAddr = remote
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := try("127.0.0.1:1", c.token); code != 200 {
		t.Errorf("authorised: %d", code)
	}
	if code := try("[::1]:1", c.token); code != 200 {
		t.Errorf("ipv6 loopback: %d", code)
	}
	if code := try("127.0.0.1:1", ""); code != 401 {
		t.Errorf("no token: %d", code)
	}
	if code := try("127.0.0.1:1", "wrong"); code != 401 {
		t.Errorf("wrong token: %d", code)
	}
	if code := try("192.168.0.10:1", c.token); code != 403 {
		t.Errorf("remote client: %d", code)
	}
	if code := try("127.0.0.1:1", c.token, "X-Forwarded-For", "203.0.113.9"); code != 403 {
		t.Errorf("forwarded: %d", code)
	}
}

func TestStartAnnouncesAndClose(t *testing.T) {
	c := newTest(t, Options{})
	if _, err := c.Start("0.0.0.0:0"); err == nil {
		t.Error("non-loopback address accepted")
	}
	addr, err := c.Start("")
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := AnnounceDir()
	path := filepath.Join(dir, strconv.Itoa(os.Getpid())+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var a Announcement
	json.Unmarshal(data, &a)
	if a.Addr != addr || a.Token != c.token || a.PID != os.Getpid() || a.Path != BasePath {
		t.Errorf("announcement: %+v", a)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("announcement mode %v", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("announcement dir mode %v", fi.Mode().Perm())
	}
	req, _ := http.NewRequest("GET", "http://"+addr+BasePath+"info", nil)
	req.Header.Set("Authorization", "Bearer "+a.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var in Info
	json.NewDecoder(resp.Body).Decode(&in)
	resp.Body.Close()
	if in.PID != os.Getpid() || in.GoVersion != runtime.Version() || in.Collector != "gitview" {
		t.Errorf("info: %+v", in)
	}
	c.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("announcement not removed")
	}
}

func TestAnnouncementIsPrivate(t *testing.T) {
	c := newTest(t, Options{})
	dir, _ := AnnounceDir()
	os.MkdirAll(dir, 0o755) // too open: tightened
	// A symlink planted at a predictable temporary name is not followed.
	victim := filepath.Join(t.TempDir(), "victim")
	os.WriteFile(victim, []byte("x"), 0o644)
	os.Symlink(victim, filepath.Join(dir, strconv.Itoa(os.Getpid())+".json.tmp"))
	if _, err := c.Start(""); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(victim); string(data) != "x" {
		t.Error("the token was written through a planted symlink")
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("directory mode %v", fi.Mode().Perm())
	}
	// An XDG_RUNTIME_DIR open to others is not used.
	open := t.TempDir()
	os.Chmod(open, 0o777)
	t.Setenv("XDG_RUNTIME_DIR", open)
	if d, _ := AnnounceDir(); strings.HasPrefix(d, open) {
		t.Errorf("announcement directory under a shared XDG_RUNTIME_DIR: %s", d)
	}
}

func TestDisabledByEnvironment(t *testing.T) {
	privateRuntimeDir(t)
	t.Setenv(EnvDisable, "off")
	c := New(Options{})
	inner := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if !c.Disabled() || c.Middleware(inner) == nil {
		t.Fatal("not disabled")
	}
	if _, err := c.Start(""); err == nil {
		t.Error("disabled collector started")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", BasePath+"info", nil)
	req.RemoteAddr = "127.0.0.1:1"
	c.Handler().ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Errorf("disabled handler answered %d", rec.Code)
	}
	if dir, _ := AnnounceDir(); func() bool { _, err := os.Stat(dir); return err == nil }() {
		t.Error("disabled collector announced itself")
	}
}

func TestProfilesAndRates(t *testing.T) {
	c := newTest(t, Options{})
	rec := call(t, c, "GET", BasePath+"profile/heap")
	if rec.Code != 200 || !bytes.HasPrefix(rec.Body.Bytes(), []byte{0x1f, 0x8b}) {
		t.Errorf("heap profile: %d %q", rec.Code, rec.Body.Bytes()[:min(4, rec.Body.Len())])
	}
	if rec := call(t, c, "GET", BasePath+"profile/nope"); rec.Code != 404 {
		t.Errorf("unknown profile: %d", rec.Code)
	}
	// Another CPU profile already running in the process is a conflict.
	if err := pprof.StartCPUProfile(io.Discard); err != nil {
		t.Fatal(err)
	}
	rec = call(t, c, "GET", BasePath+"profile/cpu?seconds=1")
	pprof.StopCPUProfile()
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "already in use") {
		t.Errorf("cpu conflict: %d %s", rec.Code, rec.Body)
	}
	// Rates need an armed capture and are restored when it ends.
	if rec := call(t, c, "POST", BasePath+"profile/rates?mutex=5"); rec.Code != 409 {
		t.Errorf("rates while idle: %d", rec.Code)
	}
	prev := runtime.SetMutexProfileFraction(-1)
	lease := arm(t, c)
	// Another GitView session can neither arm, change rates nor disarm.
	if rec := call(t, c, "POST", BasePath+"arm"); rec.Code != 409 {
		t.Errorf("second arm: %d", rec.Code)
	}
	if rec := call(t, c, "POST", BasePath+"profile/rates?mutex=5&lease=other"); rec.Code != 409 {
		t.Errorf("rates with another lease: %d", rec.Code)
	}
	if rec := call(t, c, "POST", BasePath+"disarm?lease=other"); rec.Code != 409 || c.state() == nil {
		t.Errorf("disarm with another lease: %d", rec.Code)
	}
	if rec := call(t, c, "POST", BasePath+"profile/rates?mutex=5&block=10000&lease="+lease); rec.Code != 200 {
		t.Fatalf("rates: %d %s", rec.Code, rec.Body)
	}
	if got := runtime.SetMutexProfileFraction(-1); got != 5 {
		t.Errorf("mutex fraction %d", got)
	}
	call(t, c, "POST", BasePath+"disarm?lease="+lease)
	if got := runtime.SetMutexProfileFraction(-1); got != prev {
		t.Errorf("mutex fraction not restored: %d (was %d)", got, prev)
	}
}

func TestArmExpires(t *testing.T) {
	c := newTest(t, Options{})
	c.armed.Store(&armState{until: time.Now().Add(-time.Second)})
	if c.state() != nil {
		t.Error("expired capture still armed")
	}
}

func TestRuntimeSnapshot(t *testing.T) {
	c := newTest(t, Options{})
	var snap RuntimeSnapshot
	json.Unmarshal(call(t, c, "GET", BasePath+"runtime").Body.Bytes(), &snap)
	if snap.Uint["/sched/goroutines:goroutines"] == 0 || snap.Uint["/gc/heap/allocs:bytes"] == 0 {
		t.Errorf("snapshot: %+v", snap.Uint)
	}
	if h, ok := snap.Histograms["/sched/latencies:seconds"]; !ok || len(h.Buckets) != len(h.Counts)+1 {
		t.Errorf("histogram: %+v", h)
	}
}

func TestRedactSQL(t *testing.T) {
	cases := map[string]string{
		"SELECT * FROM t WHERE a = 'x''y' AND b = 42":           "SELECT * FROM t WHERE a = ? AND b = ?",
		"select e'\\'abc' , $1, $$body$$, $q$x$q$ from t1":      "select ? , $1, ?, ? from t1",
		"INSERT INTO \"Users\" (n) VALUES (1.5e-3), (0x1F)":     "INSERT INTO \"Users\" (n) VALUES (?), (?)",
		"SELECT /* a /* nested */ c */ x -- tail\nFROM y":       "SELECT x FROM y",
		"SELECT col1, t2.c3 FROM t2 LIMIT 10 OFFSET :off":       "SELECT col1, t2.c3 FROM t2 LIMIT ? OFFSET :off",
		"UPDATE a SET p = U&'d\\0061t' , b = B'101' , c = N'x'": "UPDATE a SET p = ? , b = ? , c = ?",
		"SELECT .5, -3": "SELECT ?, -?",
		// MySQL: backslash escapes, double-quoted strings, # comments.
		`UPDATE u SET a = 'it\'s', token = 'SECRETTOKEN123'`:        "UPDATE u SET a = ?, token = ?",
		`SELECT * FROM users WHERE pw = "hunter2"`:                  "SELECT * FROM users WHERE pw = ?",
		"SELECT 1 # secret note\nFROM t":                            "SELECT ? FROM t",
		`SELECT "Name" FROM "public"."Users" JOIN "Orders" ON true`: `SELECT ? FROM "public"."Users" JOIN "Orders" ON true`,
	}
	for in, want := range cases {
		if got := RedactSQL(in); got != want {
			t.Errorf("RedactSQL(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestMaskPath(t *testing.T) {
	cases := map[string]string{
		"/clients/42": "/clients/{n}",
		"/reset/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b": "/reset/{hex}",
		"/u/123e4567-e89b-12d3-a456-426614174000":         "/u/{uuid}",
		"/blog/how-to-configure-the-reverse-proxy":        "/blog/how-to-configure-the-reverse-proxy",
		"/api/v2/oauth2/token":                            "/api/v2/oauth2/token",
		"/auth/eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U": "/auth/{value}",
		// Tokens the earlier rule kept (review): few digits, short, ":" or ";".
		"/k/aBcDeFgHiJkLmNoPqRsTuVwX":                   "/k/{value}",
		"/bot123456:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw": "/{value}",
		"/page;jsessionid=AB12CD34":                     "/{value}",
		"/pay/sk_" + "live_abcdefghijklmnopqrstuvwx":    "/pay/{value}", // split so secret scanners skip this fake key
		"/verify/482913":                                "/verify/{n}",
		"/x/Ab3dE":                                      "/x/{value}",
		"/files/{name}":                                 "/files/{name}",
	}
	for in, want := range cases {
		if got := MaskPath(in); got != want {
			t.Errorf("MaskPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// privateRuntimeDir points XDG_RUNTIME_DIR at a private (0700) temporary
// directory and the user cache at another, so that collector announcements
// never reach the real home directory.
func privateRuntimeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.Chmod(dir, 0o700)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	return dir
}
