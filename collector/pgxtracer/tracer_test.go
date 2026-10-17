package pgxtracer_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/athos/gitview/collector"
	"github.com/athos/gitview/collector/pgxtracer"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestTracerRecordsStatements(t *testing.T) {
	privateRuntimeDir(t)
	c := collector.New(collector.Options{})
	defer c.Close()
	addr, err := c.Start("")
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := collector.AnnounceDir()
	data, _ := os.ReadFile(filepath.Join(dir, itoa(os.Getpid())+".json"))
	var a collector.Announcement
	json.Unmarshal(data, &a)
	post := func(path string) *http.Response {
		req, _ := http.NewRequest("POST", "http://"+addr+collector.BasePath+path, nil)
		req.Header.Set("Authorization", "Bearer "+a.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	resp := post("arm")
	var info collector.Info
	json.NewDecoder(resp.Body).Decode(&info)
	resp.Body.Close()

	tr := &pgxtracer.Tracer{Collector: c}
	h := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := tr.TraceQueryStart(r.Context(), nil, pgx.TraceQueryStartData{SQL: "SELECT name FROM clients WHERE id = $1 AND kind = 'vip'", Args: []any{"secret-arg"}})
		tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("SELECT 3")})
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))

	req, _ := http.NewRequest("GET", "http://"+addr+collector.BasePath+"requests", nil)
	req.Header.Set("Authorization", "Bearer "+a.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var rep collector.RequestsReply
	json.NewDecoder(resp.Body).Decode(&rep)
	if len(rep.Requests) != 1 || len(rep.Requests[0].SQL) != 1 {
		t.Fatalf("reply: %+v", rep)
	}
	s := rep.Requests[0].SQL[0]
	if s.Query != "SELECT name FROM clients WHERE id = $1 AND kind = ?" || s.Rows != 3 || !s.HasRows {
		t.Errorf("span: %+v", s)
	}
	if filepath.Base(s.File) != "tracer_test.go" || !strings.Contains(s.Func, "pgxtracer_test.TestTracerRecordsStatements") {
		t.Errorf("call site: %s:%d %s", s.File, s.Line, s.Func)
	}
	// Without an armed capture nothing is recorded.
	post("disarm?lease=" + info.Lease).Body.Close()
	if ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"}); ctx != context.Background() {
		t.Error("span started while disarmed")
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
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
