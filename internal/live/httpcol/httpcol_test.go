package httpcol

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/athos/gitview/internal/live/proc"
)

func TestProxyRecordsRequests(t *testing.T) {
	var seen sync.Map
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Store(r.URL.Path, r.Header.Clone())
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusAccepted)
		w.Write(bytes.Repeat([]byte("x"), 100+len(body)))
	}))
	defer app.Close()
	var mu sync.Mutex
	var got []Request
	p, err := StartProxy(ProxyOptions{Target: app.Listener.Addr().String(), Traceparent: true, Sink: func(r Request) {
		mu.Lock()
		got = append(got, r)
		mu.Unlock()
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	req, _ := http.NewRequest("POST", "http://"+p.Addr()+"/clients/42?api_key=s3cr3t", strings.NewReader("hello"))
	req.Header.Set("Authorization", "Bearer topsecret")
	req.Header.Set("Cookie", "sid=abc")
	req.Header.Set(HeaderTraceID, "client-chosen-id")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	firstID := resp.Header.Get(HeaderTraceID)
	if resp.StatusCode != 202 || len(firstID) != 32 || firstID == "client-chosen-id" {
		t.Errorf("response %d id %q", resp.StatusCode, firstID)
	}
	resp, err = http.Get("http://" + p.Addr() + "/reset/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	generated := resp.Header.Get(HeaderTraceID)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("recorded %d requests", len(got))
	}
	r := got[0]
	if r.ID != firstID || r.W3C != firstID || r.Method != "POST" || r.Path != "/clients/{n}" || r.Status != 202 || r.Key != "POST /clients/{n}" || r.KeyBasis != "normalized path" {
		t.Errorf("request: %+v", r)
	}
	if r.BytesIn != 5 || r.BytesOut != 105 || !r.HasProxy || r.ProxyDur <= 0 || !r.HasTTFB || r.TTFB > r.ProxyDur || r.HasServer {
		t.Errorf("measures: %+v", r)
	}
	if len(generated) != 32 || got[1].ID != generated || got[1].Path != "/reset/{hex}" {
		t.Errorf("generated id %q, record %+v", generated, got[1])
	}
	// The application received the request unchanged, plus the request ID
	// (and a traceparent carrying it).
	h, _ := seen.Load("/clients/42")
	hdr := h.(http.Header)
	if hdr.Get("Authorization") != "Bearer topsecret" || hdr.Get(HeaderTraceID) != firstID || !strings.HasPrefix(hdr.Get("traceparent"), "00-"+firstID+"-") || hdr.Get("X-Forwarded-For") != "" {
		t.Errorf("forwarded headers: %v", hdr)
	}
	h2, _ := seen.Load("/reset/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b")
	if tp := h2.(http.Header).Get("traceparent"); !strings.HasPrefix(tp, "00-"+generated+"-") {
		t.Errorf("traceparent %q", tp)
	}
	data, _ := json.Marshal(got)
	for _, secret := range []string{"s3cr3t", "topsecret", "abc", "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Errorf("%q recorded", secret)
		}
	}
}

func TestProxyUpstreamDown(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	var got []Request
	var mu sync.Mutex
	p, err := StartProxy(ProxyOptions{Target: addr, Sink: func(r Request) { mu.Lock(); got = append(got, r); mu.Unlock() }})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	resp, err := http.Get("http://" + p.Addr() + "/x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	mu.Lock()
	defer mu.Unlock()
	if resp.StatusCode != 502 || len(got) != 1 || got[0].Status != 502 || !strings.Contains(got[0].Error, "connection refused") {
		t.Errorf("upstream down: %d %+v", resp.StatusCode, got)
	}
}

func TestProxyOnlyLoopback(t *testing.T) {
	if _, err := StartProxy(ProxyOptions{Target: "10.1.2.3:80"}); err == nil {
		t.Error("remote target accepted")
	}
	if _, err := StartProxy(ProxyOptions{Listen: "0.0.0.0:0", Target: "127.0.0.1:80"}); err == nil {
		t.Error("public listen address accepted")
	}
}

// The copies of the collector's functions behave like the originals (same
// cases as the collector's tests).
func TestRedactionMatchesCollector(t *testing.T) {
	sql := map[string]string{
		"SELECT * FROM t WHERE a = 'x''y' AND b = 42":           "SELECT * FROM t WHERE a = ? AND b = ?",
		"select e'\\'abc' , $1, $$body$$, $q$x$q$ from t1":      "select ? , $1, ?, ? from t1",
		"INSERT INTO \"Users\" (n) VALUES (1.5e-3), (0x1F)":     "INSERT INTO \"Users\" (n) VALUES (?), (?)",
		"SELECT /* a /* nested */ c */ x -- tail\nFROM y":       "SELECT x FROM y",
		"SELECT col1, t2.c3 FROM t2 LIMIT 10 OFFSET :off":       "SELECT col1, t2.c3 FROM t2 LIMIT ? OFFSET :off",
		"UPDATE a SET p = U&'d\\0061t' , b = B'101' , c = N'x'": "UPDATE a SET p = ? , b = ? , c = ?",
	}
	for in, want := range sql {
		if got := RedactSQL(in); got != want {
			t.Errorf("RedactSQL(%q) = %q, want %q", in, got, want)
		}
		if again := RedactSQL(want); again != want {
			t.Errorf("RedactSQL is not idempotent on %q: %q", want, again)
		}
	}
	paths := map[string]string{
		"/clients/42": "/clients/{n}",
		"/reset/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b": "/reset/{hex}",
		"/blog/how-to-configure-the-reverse-proxy":        "/blog/how-to-configure-the-reverse-proxy",
	}
	for in, want := range paths {
		if got := MaskPath(in); got != want {
			t.Errorf("MaskPath(%q) = %q", in, got)
		}
	}
	collectorSrc, err := os.ReadFile(filepath.Join("..", "..", "..", "collector", "sql.go"))
	if err != nil {
		t.Skip("collector sources not available")
	}
	mine, _ := os.ReadFile("redact.go")
	body := func(src []byte, fn string) string {
		s := string(src)
		i := strings.Index(s, "func "+fn+"(")
		if i < 0 {
			return ""
		}
		return s[i : i+strings.Index(s[i:], "\n}\n")]
	}
	collectorGo, _ := os.ReadFile(filepath.Join("..", "..", "..", "collector", "collector.go"))
	collectorSrc = append(collectorSrc, collectorGo...)
	for _, fn := range []string{"RedactSQL", "skipString", "MaskPath", "maskSegment", "isWord", "isDigits", "isHex", "isUUID", "ValidID", "NewID"} {
		if body(collectorSrc, fn) != body(mine, fn) {
			t.Errorf("%s differs from the collector's copy", fn)
		}
	}
}

func TestNormalizePath(t *testing.T) {
	cases := map[string]string{
		"/clients/42/orders/7":                    "/clients/{n}/orders/{n}",
		"/u/123e4567-e89b-12d3-a456-426614174000": "/u/{uuid}",
		"/blobs/0123456789abcdef0123":             "/blobs/{hex}",
		"/reset/{token}":                          "/reset/{token}",
		"/health":                                 "/health",
	}
	for in, want := range cases {
		if got := NormalizePath(in); got != want {
			t.Errorf("NormalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMergeBySharedID(t *testing.T) {
	t0 := time.Unix(1000, 0)
	proxy := []Request{{ID: "a1", Sources: []Source{SourceProxy}, Method: "GET", Path: "/clients/1", Status: 200, Start: t0, HasProxy: true, ProxyDur: 12 * time.Millisecond, BytesOut: 50},
		{ID: "b2", Sources: []Source{SourceProxy}, Method: "GET", Path: "/health", Status: 200, Start: t0.Add(time.Second), HasProxy: true, ProxyDur: time.Millisecond}}
	coll := []Request{{ID: "a1", Sources: []Source{SourceCollector}, Method: "GET", Path: "/clients/1", Route: "GET /clients/{id}", Status: 200, Start: t0.Add(time.Millisecond), HasServer: true, ServerDur: 10 * time.Millisecond, BytesOut: 50, InFlight: 1,
		SQL: []SQLSpan{{Query: "SELECT 1", Dur: time.Millisecond}}}}
	out := Merge(proxy, coll)
	if len(out) != 2 {
		t.Fatalf("merged: %+v", out)
	}
	r := out[0]
	if r.Key != "GET /clients/{id}" || r.KeyBasis != "route pattern" || !r.HasProxy || !r.HasServer || r.Dur() != 10*time.Millisecond || !r.Start.Equal(t0) || len(r.SQL) != 1 || len(r.Sources) != 2 {
		t.Errorf("merged request: %+v", r)
	}
	if out[1].Key != "GET /health" || out[1].Dur() != time.Millisecond {
		t.Errorf("proxy-only request: %+v", out[1])
	}
}

// protobuf encoding helpers for the OTLP tests.
func pbKey(num, wt int) []byte { return binary.AppendUvarint(nil, uint64(num<<3|wt)) }
func pbBytes(num int, b []byte) []byte {
	out := append(pbKey(num, 2), binary.AppendUvarint(nil, uint64(len(b)))...)
	return append(out, b...)
}
func pbString(num int, s string) []byte { return pbBytes(num, []byte(s)) }
func pbVarint(num int, v uint64) []byte {
	return append(pbKey(num, 0), binary.AppendUvarint(nil, v)...)
}
func pbFixed64(num int, v uint64) []byte {
	return binary.LittleEndian.AppendUint64(pbKey(num, 1), v)
}
func pbKV(k string, v []byte) []byte { return pbBytes(9, append(pbString(1, k), pbBytes(2, v)...)) }
func cat(parts ...[]byte) []byte     { return bytes.Join(parts, nil) }

func otlpFixture(pid int) []byte {
	tid, _ := hex.DecodeString("4bf92f3577b34da6a3ce929d0e0e4736")
	base := uint64(time.Unix(2000, 0).UnixNano())
	server := cat(pbBytes(1, tid), pbBytes(2, []byte{1, 2, 3, 4, 5, 6, 7, 8}), pbString(5, "GET /clients/{id}"), pbVarint(6, 2),
		pbFixed64(7, base), pbFixed64(8, base+uint64(20*time.Millisecond)),
		pbKV("http.request.method", pbString(1, "GET")), pbKV("http.route", pbString(1, "/clients/{id}")),
		pbKV("url.path", pbString(1, "/clients/42")), pbKV("http.response.status_code", pbVarint(3, 200)),
		pbKV("user.email", pbString(1, "someone@example.com")), pbKV("http.request.header.authorization", pbString(1, "Bearer x")))
	db := cat(pbBytes(1, tid), pbBytes(2, []byte{9, 9, 9, 9, 9, 9, 9, 9}), pbBytes(4, []byte{1, 2, 3, 4, 5, 6, 7, 8}), pbString(5, "query"), pbVarint(6, 3),
		pbFixed64(7, base+uint64(5*time.Millisecond)), pbFixed64(8, base+uint64(9*time.Millisecond)),
		pbKV("db.query.text", pbString(1, "SELECT name FROM clients WHERE id = 42")), pbKV("code.lineno", pbVarint(3, 31)),
		pbKV("code.filepath", pbString(1, "/src/repo/client.go")), pbKV("code.function", pbString(1, "FindByID")), pbKV("code.namespace", pbString(1, "repo.(*Repo)")),
		pbKV("db.duration", pbFixed64(4, math.Float64bits(1.5))),
		pbBytes(15, pbVarint(3, 2)))
	res := pbBytes(1, cat(pbBytes(1, cat(pbString(1, "process.pid"), pbBytes(2, pbVarint(3, uint64(pid))))),
		pbBytes(1, cat(pbString(1, "service.name"), pbBytes(2, pbString(1, "api"))))))
	scope := pbBytes(2, cat(pbBytes(1, pbString(1, "otelhttp")), pbBytes(2, server), pbBytes(2, db)))
	return pbBytes(1, cat(res, scope))
}

func TestDecodeOTLPProtobufAndAssemble(t *testing.T) {
	spans, err := DecodeOTLP(otlpFixture(77))
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 2 || spans[0].PID != 77 || spans[0].Service != "api" || spans[0].Kind != 2 || spans[1].ParentID != "0102030405060708" || !spans[1].Error {
		t.Fatalf("spans: %+v", spans)
	}
	if _, ok := spans[0].Attrs["user.email"]; ok {
		t.Error("unlisted attribute kept")
	}
	if len(spans[0].Attrs) != 4 {
		t.Errorf("attributes kept: %v", spans[0].Attrs)
	}
	if q := spans[1].Attrs["db.query.text"]; q != "SELECT name FROM clients WHERE id = ?" {
		t.Errorf("statement not redacted: %q", q)
	}
	reqs, _ := AssembleOTel(spans, 77, false)
	if len(reqs) != 1 {
		t.Fatalf("requests: %+v", reqs)
	}
	r := reqs[0]
	if r.ID != "4bf92f3577b34da6a3ce929d0e0e4736" || r.Method != "GET" || r.Route != "/clients/{id}" || r.Key != "GET /clients/{id}" || r.Status != 200 || r.ServerDur != 20*time.Millisecond || r.Path != "/clients/{n}" {
		t.Errorf("request: %+v", r)
	}
	if len(r.SQL) != 1 || r.SQL[0].Start != 5*time.Millisecond || r.SQL[0].Dur != 4*time.Millisecond || r.SQL[0].Line != 31 || r.SQL[0].Func != "repo.(*Repo).FindByID" || r.SQL[0].Err == "" {
		t.Errorf("sql: %+v", r.SQL)
	}
	if other, _ := AssembleOTel(spans, 78, false); len(other) != 0 {
		t.Error("spans of another process accepted")
	}
}

func TestOTLPReceiverJSON(t *testing.T) {
	var mu sync.Mutex
	var got []OTelSpan
	o, err := StartOTLP("127.0.0.1:0", func(s []OTelSpan) { mu.Lock(); got = append(got, s...); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	body := `{"resourceSpans":[{"resource":{"attributes":[{"key":"process.pid","value":{"intValue":"55"}}]},
	 "scopeSpans":[{"spans":[{"traceId":"5B8EFFF798038103D269B633813FC60C","spanId":"EEE19B7EC3C1B174","name":"GET /x","kind":2,
	 "startTimeUnixNano":"1544712660000000000","endTimeUnixNano":"1544712661000000000",
	 "attributes":[{"key":"http.method","value":{"stringValue":"GET"}},{"key":"http.target","value":{"stringValue":"/x?token=abc"}},{"key":"http.status_code","value":{"intValue":500}}]}]}]}]}`
	resp, err := http.Post("http://"+o.Addr()+"/v1/traces", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	reply, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(reply) != "{}" {
		t.Fatalf("reply %d %s", resp.StatusCode, reply)
	}
	resp, _ = http.Post("http://"+o.Addr()+"/v1/traces", "application/x-protobuf", bytes.NewReader(otlpFixture(55)))
	resp.Body.Close()
	mu.Lock()
	defer mu.Unlock()
	reqs, _ := AssembleOTel(got, 55, false)
	if len(reqs) != 2 || reqs[0].ID != "5b8efff798038103d269b633813fc60c" || reqs[0].Path != "/x" || reqs[0].Status != 500 || reqs[0].ServerDur != time.Second {
		t.Errorf("json request: %+v", reqs)
	}
}

// fakeCollector serves the collector protocol for the client tests.
func fakeCollector(t *testing.T, pid int, token string) (*httptest.Server, *[]string) {
	var calls []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(401)
			return
		}
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		switch r.URL.Path {
		case "/debug/gitview/info", "/debug/gitview/arm":
			json.NewEncoder(w).Encode(CollectorInfo{Collector: "gitview", Version: 1, PID: pid, GoVersion: "go1.25.0", Armed: r.URL.Path == "/debug/gitview/arm"})
		case "/debug/gitview/requests":
			w.Write([]byte(`{"pid":` + strconv.Itoa(pid) + `,"last":2,"requests":[{"seq":2,"id":"abc12345","method":"GET","path":"/clients/9","route":"GET /clients/{id}","status":200,"start":1700000000000000000,"dur":3000000,"bytesOut":10,"inFlight":1,"labelled":true,
			  "sql":[{"query":"SELECT 1 WHERE x = 'leak'","start":1000,"dur":2000,"file":"/src/r.go","line":7,"func":"r.F","rows":1,"hasRows":true}],"runtime":{"gcCycles":1}}],
			  "orphans":[{"query":"DELETE FROM jobs","start":1700000000500000000,"dur":100}],"orphansLast":1}`))
		default:
			w.WriteHeader(404)
		}
	}))
	return srv, &calls
}

func TestFindCollectorAndRead(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0o700)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	pid := 4321
	srv, calls := fakeCollector(t, pid, "tok")
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	n, _ := strconv.Atoi(port)
	p := &proc.Process{PID: pid, Started: time.Now().Add(-time.Minute), Ports: []proc.Port{{Proto: "tcp", Addr: "127.0.0.1", Port: n, Loopback: true}}}

	if d := FindCollector(context.Background(), p); d.Collector != nil || !strings.Contains(strings.Join(d.Notes, ";"), "no GitView collector announced") {
		t.Errorf("without announcement: %+v", d)
	}
	ann := filepath.Join(dir, "gitview", "collectors")
	os.MkdirAll(ann, 0o700)
	write := func(a announcement) {
		data, _ := json.Marshal(a)
		os.WriteFile(filepath.Join(ann, strconv.Itoa(pid)+".json"), data, 0o600)
	}
	// An announcement older than the process is stale.
	write(announcement{Collector: "gitview", Version: 1, PID: pid, Addr: srv.Listener.Addr().String(), Path: "/debug/gitview/", Token: "tok", Started: time.Now().Add(-time.Hour).UnixNano()})
	if d := FindCollector(context.Background(), p); d.Collector != nil || !strings.Contains(strings.Join(d.Notes, ";"), "stale") {
		t.Errorf("stale announcement: %+v", d)
	}
	// An address the process does not own is refused.
	write(announcement{Collector: "gitview", Version: 1, PID: pid, Addr: "127.0.0.1:1", Path: "/debug/gitview/", Token: "tok", Started: time.Now().UnixNano()})
	if d := FindCollector(context.Background(), p); d.Collector != nil || !strings.Contains(strings.Join(d.Notes, ";"), "not a port of process") {
		t.Errorf("foreign address: %+v", d)
	}
	write(announcement{Collector: "gitview", Version: 1, PID: pid, Addr: srv.Listener.Addr().String(), Path: "/debug/gitview/", Token: "tok", Started: time.Now().UnixNano()})
	d := FindCollector(context.Background(), p)
	if d.Collector == nil || d.Info.GoVersion != "go1.25.0" {
		t.Fatalf("discovery: %+v", d)
	}
	c := d.Collector
	if _, err := c.Arm(context.Background(), ArmOptions{TTL: 90 * time.Second, Labels: true}); err != nil {
		t.Fatal(err)
	}
	pg, err := c.Requests(context.Background(), 0, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if pg.Last != 2 || len(pg.Requests) != 1 || len(pg.Orphans) != 1 || pg.OrphansLast != 1 {
		t.Fatalf("page: %+v", pg)
	}
	r := pg.Requests[0]
	if r.Key != "GET /clients/{id}" || r.ServerDur != 3*time.Millisecond || !r.HasServer || r.HasProxy || !r.HasGC || r.GCDuring != 1 || !r.Labelled {
		t.Errorf("request: %+v", r)
	}
	if len(r.SQL) != 1 || r.SQL[0].Query != "SELECT ? WHERE x = ?" || r.SQL[0].Line != 7 || r.SQL[0].Source != SourceCollector {
		t.Errorf("sql: %+v", r.SQL)
	}
	if got := strings.Join(*calls, "\n"); !strings.Contains(got, "POST /debug/gitview/arm?ttl=90&labels=1&tasks=0&runtime=0") || !strings.Contains(got, "requests?since=0&orphansSince=0&max=100") {
		t.Errorf("calls:\n%s", got)
	}
	// A collector in another PID namespace reports another pid.
	srv2, _ := fakeCollector(t, 1, "tok")
	defer srv2.Close()
	_, port2, _ := net.SplitHostPort(srv2.Listener.Addr().String())
	n2, _ := strconv.Atoi(port2)
	p.Ports = append(p.Ports, proc.Port{Proto: "tcp", Addr: "127.0.0.1", Port: n2})
	write(announcement{Collector: "gitview", Version: 1, PID: pid, Addr: srv2.Listener.Addr().String(), Path: "/debug/gitview/", Token: "tok", Started: time.Now().UnixNano()})
	if d := FindCollector(context.Background(), p); d.Collector != nil || !strings.Contains(strings.Join(d.Notes, ";"), "reports pid 1") {
		t.Errorf("pid mismatch: %+v", d)
	}
}

func TestSharedTraceDoesNotCollapseRequests(t *testing.T) {
	t0 := time.Unix(5000, 0)
	span := func(id, parent string, kind int, start, end time.Duration, attrs map[string]string) OTelSpan {
		return OTelSpan{TraceID: "aaaa1111aaaa1111aaaa1111aaaa1111", SpanID: id, ParentID: parent, Kind: kind, Start: t0.Add(start), End: t0.Add(end), Attrs: attrs, PID: 9}
	}
	get := map[string]string{"http.request.method": "GET", "url.path": "/clients/1", "http.response.status_code": "200"}
	spans := []OTelSpan{
		span("s1", "", 2, 0, 10*time.Millisecond, get),
		span("d1", "s1", 3, 2*time.Millisecond, 4*time.Millisecond, map[string]string{"db.statement": "SELECT 1"}),
		span("s2", "", 2, 50*time.Millisecond, 60*time.Millisecond, get),
		span("d2", "s2", 3, 52*time.Millisecond, 53*time.Millisecond, map[string]string{"db.statement": "SELECT 2"}),
		// A span of an unidentified process is ignored by default.
		{TraceID: "bbbb", SpanID: "x", Kind: 2, Attrs: get, Start: t0, End: t0.Add(time.Millisecond)},
	}
	reqs, unidentified := AssembleOTel(spans, 9, false)
	if len(reqs) != 2 || unidentified != 1 || reqs[0].ID == reqs[1].ID || len(reqs[0].SQL) != 1 || reqs[0].SQL[0].Query != "SELECT ?" || reqs[1].SQL[0].Start != 2*time.Millisecond {
		t.Fatalf("requests %+v unidentified %d", reqs, unidentified)
	}
	if all, _ := AssembleOTel(spans, 9, true); len(all) != 3 {
		t.Errorf("accepting unidentified spans: %d", len(all))
	}
	// The client sent its own traceparent: the proxy's records have other
	// IDs, and each OTel request joins the proxy record it overlaps.
	proxy := []Request{
		{ID: "p1", W3C: "aaaa1111aaaa1111aaaa1111aaaa1111", Sources: []Source{SourceProxy}, Method: "GET", Path: "/clients/{n}", Start: t0.Add(-time.Millisecond), HasProxy: true, ProxyDur: 12 * time.Millisecond},
		{ID: "p2", W3C: "aaaa1111aaaa1111aaaa1111aaaa1111", Sources: []Source{SourceProxy}, Method: "GET", Path: "/clients/{n}", Start: t0.Add(49 * time.Millisecond), HasProxy: true, ProxyDur: 12 * time.Millisecond},
	}
	out := Merge(proxy, reqs)
	if len(out) != 2 || !out[0].HasServer || !out[1].HasServer || len(out[0].SQL) != 1 || out[0].ID != "p1" {
		t.Errorf("merged: %+v", out)
	}
	// Two records of one source with the same ID stay two requests.
	dup := Merge([]Request{{ID: "x", Sources: []Source{SourceCollector}, Start: t0}, {ID: "x", Sources: []Source{SourceCollector}, Start: t0.Add(time.Second)}})
	if len(dup) != 2 || dup[0].ID == dup[1].ID {
		t.Errorf("same-source duplicates: %+v", dup)
	}
}

func TestAnnouncementAndClientHardening(t *testing.T) {
	for p, ok := range map[string]bool{"/debug/gitview/": true, "/x/": true, "@evil.example:80/": false, "/a//b/": false, "/../": false, "/x": false, "/x?y/": false} {
		if validPath(p) != ok {
			t.Errorf("validPath(%q) = %v", p, !ok)
		}
	}
	dir := t.TempDir()
	os.Chmod(dir, 0o700)
	good := filepath.Join(dir, "1.json")
	os.WriteFile(good, []byte("{}"), 0o600)
	if _, err := readAnnouncement(good); err != nil {
		t.Errorf("private file refused: %v", err)
	}
	open := filepath.Join(dir, "2.json")
	os.WriteFile(open, []byte("{}"), 0o644)
	if _, err := readAnnouncement(open); err == nil {
		t.Error("group-readable announcement accepted")
	}
	link := filepath.Join(dir, "3.json")
	os.Symlink(good, link)
	if _, err := readAnnouncement(link); err == nil {
		t.Error("symlinked announcement accepted")
	}

	// The client neither dials other hosts nor follows redirects.
	hc := LoopbackClient(time.Second)
	if _, err := hc.Get("http://10.255.255.1:9/"); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Errorf("non-loopback dial: %v", err)
	}
	away := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/", http.StatusFound)
	}))
	defer away.Close()
	resp, err := hc.Get(away.URL)
	if err != nil || resp.StatusCode != http.StatusFound {
		t.Errorf("redirect followed: %v %v", resp, err)
	} else {
		resp.Body.Close()
	}
}

func TestAnnouncedAddressMustBeLoopback(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0o700)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	ann := filepath.Join(dir, "gitview", "collectors")
	os.MkdirAll(ann, 0o700)
	p := &proc.Process{PID: 77, Started: time.Now().Add(-time.Minute), Ports: []proc.Port{{Port: 8080, Addr: "0.0.0.0"}}}
	data, _ := json.Marshal(announcement{Collector: "gitview", Version: 1, PID: 77, Addr: "10.1.2.3:8080", Path: "/debug/gitview/", Token: "t", Started: time.Now().UnixNano()})
	os.WriteFile(filepath.Join(ann, "77.json"), data, 0o600)
	if d := FindCollector(context.Background(), p); d.Collector != nil || !strings.Contains(strings.Join(d.Notes, ";"), "not a port of process") {
		t.Errorf("remote announced address: %+v", d)
	}
	data, _ = json.Marshal(announcement{Collector: "gitview", Version: 1, PID: 77, Addr: "127.0.0.1:8080", Path: "@evil.example:80/", Token: "t", Started: time.Now().UnixNano()})
	os.WriteFile(filepath.Join(ann, "77.json"), data, 0o600)
	if d := FindCollector(context.Background(), p); d.Collector != nil || !strings.Contains(strings.Join(d.Notes, ";"), "invalid endpoint path") {
		t.Errorf("hostile path: %+v", d)
	}
	p.OwnNetNS = true
	data, _ = json.Marshal(announcement{Collector: "gitview", Version: 1, PID: 77, Addr: "127.0.0.1:8080", Path: "/debug/gitview/", Token: "t", Started: time.Now().UnixNano()})
	os.WriteFile(filepath.Join(ann, "77.json"), data, 0o600)
	if d := FindCollector(context.Background(), p); d.Collector != nil || !strings.Contains(strings.Join(d.Notes, ";"), "network namespace") {
		t.Errorf("container: %+v", d)
	}
}
