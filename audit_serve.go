package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/athos/gitview/internal/audit"
	"github.com/athos/gitview/internal/audit/ai"
	"github.com/athos/gitview/internal/export"
	gexport "github.com/athos/gitview/internal/graph/export"
	gmodel "github.com/athos/gitview/internal/graph/model"
)

func signalContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt)
}

// securityPage renders the Security Journey: the Repository Graph page
// with the audit embedded and, instead of whole source files, only the
// snippets the findings point to.
func securityPage(g *gmodel.Graph, a *audit.Audit, store *audit.Store, root string, live *gexport.Live) ([]byte, error) {
	reviews, err := store.Reviews()
	if err != nil {
		return nil, err
	}
	var diff *audit.Diff
	if list, err := store.Snapshots(); err == nil && len(list) >= 2 {
		for i := len(list) - 1; i >= 0; i-- {
			prev, _, err := store.Load(list[i].ID)
			if err != nil || prev.Generated.Equal(a.Generated) || !prev.Generated.Before(a.Generated) {
				continue
			}
			diff = audit.Compare(prev, a)
			break
		}
	}
	inv := map[string]json.RawMessage{}
	for _, f := range a.Findings {
		var raw json.RawMessage
		if store.LatestInvestigation(f.ID, &raw) {
			inv[f.ID] = raw
		}
	}
	read := repoReader(root)
	page := audit.BuildPage(a, reviews, func(file string) ([]string, error) { return read(file) }, diff, inv)
	page.Live = live != nil
	data, err := json.Marshal(page)
	if err != nil {
		return nil, err
	}
	audit.Annotate(g, a)
	var buf bytes.Buffer
	err = gexport.HTML(&buf, g, gexport.HTMLOptions{NoSource: true, Security: data, Live: live})
	return buf.Bytes(), err
}

func runAuditExport(ctx context.Context, stdout, stderr io.Writer, store *audit.Store, dir string, f auditFlags) int {
	if f.format != "html" && f.format != "json" {
		fmt.Fprintln(stderr, "gitview audit export: --format deve ser html ou json")
		return 2
	}
	out := f.output
	if out == "" && f.format == "html" {
		out = "gitview-security.html"
	}
	// Refuse before analysing, not after.
	if out != "" && !f.overwrite {
		if _, err := os.Stat(out); err == nil {
			fmt.Fprintf(stderr, "gitview audit export: %s já existe; use --overwrite\n", out)
			return 1
		}
	}
	a, g, err := auditScan(ctx, dir, f, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "gitview audit export:", err)
		return 1
	}
	if _, err := store.Save(a); err != nil {
		fmt.Fprintln(stderr, "gitview audit export: snapshot:", err)
		return 1
	}
	root, _ := repoRoot(dir)
	var data []byte
	if f.format == "json" {
		reviews, _ := store.Reviews()
		page := audit.BuildPage(a, reviews, repoReader(root), nil, nil)
		data, err = json.MarshalIndent(page, "", "  ")
		data = append(data, '\n')
	} else {
		data, err = securityPage(g, a, store, root, nil)
	}
	if err != nil {
		fmt.Fprintln(stderr, "gitview audit export:", err)
		return 1
	}
	if out == "" {
		_, err = stdout.Write(data)
		if err != nil {
			return 1
		}
		return 0
	}
	if err := export.WriteFile(out, data, f.overwrite); err != nil {
		fmt.Fprintln(stderr, "gitview audit export:", err)
		return 1
	}
	fmt.Fprintf(stderr, "relatório gravado em %s (%s) · %d findings\n", out, humanBytes(int64(len(data))), len(a.Findings))
	return 0
}

// ---------- local server ----------

type auditServer struct {
	mu     sync.Mutex
	root   string
	store  *audit.Store
	g      *gmodel.Graph
	a      *audit.Audit
	inv    *ai.Investigator
	invErr error
	token  string
	host   string // 127.0.0.1:port
	model  string
}

func newToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func runAuditServe(ctx context.Context, stdout, stderr io.Writer, store *audit.Store, dir string, f auditFlags) int {
	a, g, err := auditScan(ctx, dir, f, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "gitview audit serve:", err)
		return 1
	}
	if _, err := store.Save(a); err != nil {
		fmt.Fprintln(stderr, "gitview audit serve: snapshot:", err)
		return 1
	}
	root, _ := repoRoot(dir)
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(f.port)))
	if err != nil {
		fmt.Fprintln(stderr, "gitview audit serve:", err)
		return 1
	}
	srv := &auditServer{root: root, store: store, g: g, a: a, token: newToken(), host: ln.Addr().String(), model: f.ai.model}
	srv.inv, srv.invErr = newInvestigator(store, root, f.ai)
	hs := &http.Server{Handler: srv.handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	url := "http://" + srv.host + "/?t=" + srv.token
	fmt.Fprintf(stdout, "Security Journey em %s\n(só esta máquina; Ctrl+C encerra)\n", url)
	if srv.invErr != nil {
		fmt.Fprintf(stdout, "IA indisponível: %v\n", srv.invErr)
	}
	if f.open {
		_ = startBrowser(url)
	}
	ctx, stop := signalContext(ctx)
	defer stop()
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		hs.Shutdown(sh)
	}()
	if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(stderr, "gitview audit serve:", err)
		return 1
	}
	return 0
}

func (s *auditServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.page)
	mux.HandleFunc("GET /api/ai/status", s.api(s.aiStatus))
	mux.HandleFunc("POST /api/explain", s.api(s.explain))
	mux.HandleFunc("POST /api/explain-path", s.api(s.explainPath))
	mux.HandleFunc("POST /api/review", s.api(s.review))
	return s.guard(mux)
}

// guard accepts only requests addressed to the local server by name
// (against DNS rebinding) and, for the API, with the token and from the
// page's own origin.
func (s *auditServer) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, port, _ := net.SplitHostPort(s.host)
		if r.Host != s.host && r.Host != "localhost:"+port {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if o := r.Header.Get("Origin"); o != "" && o != "http://"+s.host && o != "http://localhost:"+port {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-GitView-Token")), []byte(s.token)) != 1 {
				http.Error(w, "missing or wrong token", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *auditServer) page(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("t")), []byte(s.token)) != 1 {
		http.Error(w, "open the address printed by gitview audit serve", http.StatusForbidden)
		return
	}
	s.mu.Lock()
	data, err := securityPage(s.g, s.a, s.store, s.root, &gexport.Live{Token: s.token})
	s.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

type apiFunc func(r *http.Request, body []byte) (any, int, error)

func (s *auditServer) api(fn apiFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if r.Method == http.MethodPost {
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				http.Error(w, "JSON expected", http.StatusUnsupportedMediaType)
				return
			}
			var err error
			body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
			if err != nil {
				http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
				return
			}
		}
		v, code, err := fn(r, body)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(code)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(v)
	}
}

func (s *auditServer) aiStatus(r *http.Request, _ []byte) (any, int, error) {
	if s.invErr != nil {
		return ai.Status{Error: s.invErr.Error()}, 0, nil
	}
	return s.inv.Client.Probe(r.Context()), 0, nil
}

func (s *auditServer) explain(r *http.Request, body []byte) (any, int, error) {
	var req struct {
		Finding string `json:"finding"`
		Model   string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, http.StatusBadRequest, err
	}
	if s.invErr != nil {
		return nil, http.StatusServiceUnavailable, s.invErr
	}
	fd := s.a.Finding(req.Finding)
	if fd == nil {
		return nil, http.StatusNotFound, errors.New("unknown finding")
	}
	model := req.Model
	if model == "" {
		model = s.model
	}
	res, err := s.inv.ExplainFinding(r.Context(), ai.InvestigationRequest{Finding: fd, Model: model})
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	return res, 0, nil
}

func (s *auditServer) explainPath(r *http.Request, body []byte) (any, int, error) {
	var req struct {
		From, To, Model string
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, http.StatusBadRequest, err
	}
	if s.invErr != nil {
		return nil, http.StatusServiceUnavailable, s.invErr
	}
	p, err := audit.PathBetween(s.g, s.a, req.From, req.To)
	if err != nil {
		return nil, http.StatusUnprocessableEntity, err
	}
	model := req.Model
	if model == "" {
		model = s.model
	}
	res, err := s.inv.ExplainPath(r.Context(), pathRequest(p, req.From, req.To, model))
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	return res, 0, nil
}

func (s *auditServer) review(r *http.Request, body []byte) (any, int, error) {
	var req struct {
		Finding, Status, Note string
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, http.StatusBadRequest, err
	}
	fd := s.a.Finding(req.Finding)
	if fd == nil {
		return nil, http.StatusNotFound, errors.New("unknown finding")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	reviews, err := s.store.Reviews()
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	rec, err := reviews.Decide(fd, req.Status, req.Note, s.a.Commit, time.Now())
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	if err := s.store.SaveReviews(reviews); err != nil {
		return nil, http.StatusInternalServerError, err
	}
	return map[string]any{"record": rec, "revalidate": reviews.Revalidation(fd)}, 0, nil
}
