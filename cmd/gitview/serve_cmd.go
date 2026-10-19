package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/athos/gitview/internal/audit"
	"github.com/athos/gitview/internal/audit/ai"
	"github.com/athos/gitview/internal/export"
	"github.com/athos/gitview/internal/flow"
	gexport "github.com/athos/gitview/internal/graph/export"
	gmodel "github.com/athos/gitview/internal/graph/model"
	"github.com/athos/gitview/internal/inspect"
	"github.com/athos/gitview/internal/live/capture"
	"github.com/athos/gitview/internal/live/report"
	"github.com/athos/gitview/internal/server"
	"github.com/athos/gitview/internal/webui"
)

const serveUsage = `uso: gitview serve [opções] [diretório]

Abre o GitView no navegador: um servidor local (só 127.0.0.1) com a
interface web sobre o repositório do diretório (padrão: o atual):

  Architecture  o Repository Graph navegável (pacotes, tipos, funções, chamadas)
  HTTP          rotas, contratos e o caminho de cada rota pelo código
  Security      o CodeAudit: findings, evidências, revisão e IA local (Ollama)
  Changes       os diffs do working tree
  Code Flow     o call graph a partir das funções alteradas
  Inspector     CFG, Data Flow e SQL das funções alteradas
  Study         a ordem de leitura e os exercícios das mudanças
  Runtime       as capturas salvas por gitview runtime capture

As análises rodam em segundo plano e ficam em cache; quando o working tree
muda, a página oferece analisar de novo. Nada do repositório é executado ou
modificado. A URL impressa tem um token aleatório: só ela abre a página.

exemplos:
  gitview serve
  gitview serve ~/dev/api --port 8080
  gitview serve --view security --model gemma3:4b
  gitview serve --open=false
`

type serveOptions struct {
	audit auditFlags
	view  string
	watch bool
}

func runServe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o serveOptions
	f := &o.audit
	fs.StringVar(&f.module, "module", "", "analisa só o módulo Go deste diretório (padrão: todos os módulos do repositório)")
	fs.StringVar(&f.state, "state", os.Getenv("GITVIEW_AUDIT_DIR"), "diretório dos snapshots e revisões do CodeAudit (padrão: cache do GitView)")
	fs.StringVar(&f.vulndb, "vulndb", os.Getenv("GITVIEW_VULNDB"), "cópia local da Go vulnerability database (OSV) para GO-DEP-001")
	fs.IntVar(&f.port, "port", 0, "porta local (padrão: uma porta livre)")
	fs.BoolVar(&f.open, "open", true, "abre o navegador")
	fs.StringVar(&o.view, "view", "", "visão inicial: architecture, http, security, changes, flow, inspect, study ou runtime")
	noWatch := fs.Bool("no-watch", false, "não observa o working tree")
	f.ai.register(fs)
	fs.Usage = func() { fmt.Fprint(stderr, serveUsage+"\nopções:\n"); fs.PrintDefaults() }
	pos, err := parseFlags(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(pos) > 1 {
		fs.Usage()
		return 2
	}
	dir := "."
	if len(pos) == 1 {
		dir = pos[0]
	}
	o.watch = !*noWatch
	ctx, stop := signalContext(context.Background())
	defer stop()
	return serveRepo(ctx, stdout, stderr, dir, o)
}

// serveRepo serves the web UI over the repository of dir until ctx ends.
func serveRepo(ctx context.Context, stdout, stderr io.Writer, dir string, o serveOptions) int {
	root, err := repoRoot(dir)
	if err != nil {
		fmt.Fprintln(stderr, "gitview serve:", err)
		return 1
	}
	store, err := audit.StoreFor(root, o.audit.state)
	if err != nil {
		fmt.Fprintln(stderr, "gitview serve:", err)
		return 1
	}
	w := &workspace{dir: dir, root: root, f: o.audit, store: store, progress: stderr, cache: flow.NewCache()}
	w.inv, w.invErr = newInvestigator(store, root, o.audit.ai)
	w.layout, _ = layoutFile(root)
	var changes chan struct{}
	if o.watch {
		if ev, err := watch(root); err == nil {
			changes = make(chan struct{}, 1)
			go w.forward(ctx, ev, changes)
		}
	}
	srv := w.server(o.view, changes)
	ln, err := srv.Listen(o.audit.port)
	if err != nil {
		fmt.Fprintln(stderr, "gitview serve:", err)
		return 1
	}
	fmt.Fprintf(stdout, "GitView em %s\n(só esta máquina; Ctrl+C encerra)\n", srv.URL())
	if w.invErr != nil {
		fmt.Fprintf(stdout, "IA indisponível: %v\n", w.invErr)
	}
	if o.audit.open {
		_ = startBrowser(srv.URL())
	}
	srv.Warm(ctx)
	if err := srv.Serve(ctx, ln); err != nil {
		fmt.Fprintln(stderr, "gitview serve:", err)
		return 1
	}
	return 0
}

// workspace holds the analyses of the served repository. The Repository
// Graph and the CodeAudit come from one analysis, shared by the graph and
// security sections and redone only after the working tree changed.
type workspace struct {
	dir, root string
	f         auditFlags
	store     *audit.Store
	progress  io.Writer
	cache     *flow.Cache
	inv       *ai.Investigator
	invErr    error
	layout    string // file of the Architecture view's saved card positions

	mu      sync.Mutex
	dirty   bool
	repo    *repoAnalysis
	repoErr error
	running chan struct{}
	reviews sync.Mutex
}

type repoAnalysis struct {
	g        *gmodel.Graph
	a        *audit.Audit // nil when the audit failed
	auditErr error
	graph    map[string]any
	sources  string
	security json.RawMessage
}

// server is the local server over the workspace's analyses.
func (w *workspace) server(view string, changes <-chan struct{}) *server.Server {
	return server.New(server.Config{
		Repository: filepath.Base(w.root), Root: w.root, Branch: w.branch, View: view,
		Sections: map[string]server.Analyzer{
			webui.SectionGraph:    w.graphSection,
			webui.SectionSecurity: w.securitySection,
			webui.SectionReport:   w.reportSection,
			webui.SectionRuntime:  w.runtimeSection,
		},
		Order:   []string{webui.SectionGraph, webui.SectionSecurity, webui.SectionReport},
		Sources: w.sources,
		API: map[string]server.APIFunc{
			"GET /api/ai/status":            w.aiStatus,
			"POST /api/explain":             w.explain,
			"POST /api/explain-path":        w.explainPath,
			"POST /api/review":              w.review,
			"GET /api/runtime/capture/{id}": w.runtimeCapture,
			"GET /api/runtime/compare":      w.runtimeCompare,
		},
		Changes: changes,
		// Development only (make dev): a fixed token gives the Vite dev
		// server a stable URL.
		Token:       os.Getenv("GITVIEW_SERVE_TOKEN"),
		LayoutFile:  w.layout,
		ParseLayout: gexport.ParseLayout,
		MaxLayout:   gexport.MaxLayoutSize,
	})
}

func (w *workspace) forward(ctx context.Context, ev <-chan struct{}, out chan<- struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ev:
			if !ok {
				return
			}
			w.mu.Lock()
			w.dirty = true
			w.mu.Unlock()
			select {
			case out <- struct{}{}:
			default:
			}
		}
	}
}

func (w *workspace) branch() string {
	if repo, err := OpenRepo(w.root); err == nil {
		return repo.Branch()
	}
	return ""
}

// analysis returns the repository analysis, running it once at a time.
func (w *workspace) analysis(ctx context.Context) (*repoAnalysis, error) {
	w.mu.Lock()
	for w.running != nil {
		ch := w.running
		w.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		w.mu.Lock()
	}
	if (w.repo != nil || w.repoErr != nil) && !w.dirty {
		r, err := w.repo, w.repoErr
		w.mu.Unlock()
		return r, err
	}
	ch := make(chan struct{})
	w.running, w.dirty = ch, false
	w.mu.Unlock()

	r, err := w.analyse(ctx)

	w.mu.Lock()
	w.repo, w.repoErr, w.running = r, err, nil
	w.mu.Unlock()
	close(ch)
	return r, err
}

func (w *workspace) analyse(ctx context.Context) (*repoAnalysis, error) {
	root, progs, failures, took, err := loadGraphPrograms(ctx, w.dir, w.f.module, w.progress)
	if err != nil {
		return nil, err
	}
	g, err := buildRepoGraph(ctx, root, progs, failures, took, false)
	if err != nil {
		return nil, err
	}
	r := &repoAnalysis{g: g}
	if r.a, r.auditErr = auditGraph(ctx, root, progs, g, w.f); r.auditErr == nil {
		if _, err := w.store.Save(r.a); err != nil {
			fmt.Fprintln(w.progress, "gitview serve: snapshot do CodeAudit:", err)
		}
		if r.security, r.auditErr = securityData(r.a, w.store, w.root, true); r.auditErr == nil {
			audit.Annotate(g, r.a)
		}
	}
	graph, src, err := gexport.GraphSection(g, gexport.HTMLOptions{})
	if err != nil {
		return nil, err
	}
	r.graph, r.sources = graph, src.Encoded
	return r, nil
}

func (w *workspace) graphSection(ctx context.Context) (any, error) {
	r, err := w.analysis(ctx)
	if err != nil {
		return nil, err
	}
	return r.graph, nil
}

func (w *workspace) securitySection(ctx context.Context) (any, error) {
	r, err := w.analysis(ctx)
	if err != nil {
		return nil, err
	}
	if r.auditErr != nil {
		return nil, fmt.Errorf("CodeAudit: %w", r.auditErr)
	}
	return r.security, nil
}

func (w *workspace) sources(ctx context.Context) (string, error) {
	r, err := w.analysis(ctx)
	if err != nil {
		return "", err
	}
	return r.sources, nil
}

// reportSection is the session report of `gitview study`: the working
// tree's diffs, Code Flow from the changed functions, the Deep Inspector
// of the reading order and Study Mode. No test is run.
func (w *workspace) reportSection(ctx context.Context) (any, error) {
	rep := export.Report{Project: filepath.Base(w.root), Root: w.root, GeneratedAt: time.Now(), Scope: "gitview serve",
		Tests: &export.TestReport{Ran: false}}
	changed := map[string]map[int]bool{}
	var files []FileChange
	if repo, err := OpenRepo(w.root); err == nil {
		rep.Branch = repo.Branch()
		if c := repo.HeadCommit(); c != nil {
			rep.Base = &export.Commit{Hash: c.Hash, Short: c.Short, Subject: c.Subject, Author: c.Author, Date: c.Date}
		}
		if fc, err := repo.Changes(); err == nil {
			files = fc
			changed = changedFromFiles(fc, func(f FileChange) *parsedDiff { return parseDiff(f.Path, f.Diff) })
		} else {
			rep.Warnings = append(rep.Warnings, "git: "+err.Error())
		}
	}
	for _, fc := range files {
		gf := gitdiffFile(fc)
		if pd := parseDiff(fc.Path, fc.Diff); pd != nil {
			gf.Hunks = pd.hunks
		}
		rep.Files = append(rep.Files, gf)
	}
	rep.Files, rep.Omitted = export.Redact(rep.Files, false)
	progs, failures := loadRepoPrograms(ctx, w.root, w.cache)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(progs) == 0 {
		if len(failures) > 0 {
			rep.Flow = &export.FlowData{Error: "Go analysis failed: " + failures[0]}
		}
		return export.NewReportSection(rep), nil
	}
	sess := inspect.NewSession(w.root, progs)
	buildStudyReport(ctx, &rep, w.root, progs, failures, changed, files, sess, sess.Changed(changed))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return export.NewReportSection(rep), nil
}

// ---------- runtime captures ----------

func (w *workspace) runtimeEnv(ctx context.Context) *runtimeEnv {
	return &runtimeEnv{ctx: ctx, dir: w.root, stdout: io.Discard, stderr: io.Discard}
}

// runtimeSection is the latest saved capture, with the list of captures.
func (w *workspace) runtimeSection(ctx context.Context) (any, error) {
	e := w.runtimeEnv(ctx)
	st, err := e.store(w.root)
	if err != nil {
		return nil, err
	}
	list, err := st.List()
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []capture.Entry{}
	}
	var a *report.Analysis
	if len(list) > 0 {
		if a, err = w.analyseCapture(e, st, list[0].ID); err != nil {
			return nil, err
		}
	}
	s := export.NewRuntimeSection(a, nil)
	s.Captures = list
	return s, nil
}

func (w *workspace) analyseCapture(e *runtimeEnv, st *capture.Store, id string) (*report.Analysis, error) {
	c, err := st.Load(id)
	if err != nil {
		return nil, err
	}
	return report.Analyze(c, e.mapper(w.root, c.Process.MainModule))
}

func (w *workspace) runtimeCapture(r *http.Request, _ []byte) (any, int, error) {
	e := w.runtimeEnv(r.Context())
	st, err := e.store(w.root)
	if err != nil {
		return nil, http.StatusServiceUnavailable, err
	}
	a, err := w.analyseCapture(e, st, r.PathValue("id"))
	if err != nil {
		return nil, http.StatusNotFound, err
	}
	s := export.NewRuntimeSection(a, nil)
	if list, err := st.List(); err == nil {
		s.Captures = list
	}
	return s, 0, nil
}

func (w *workspace) runtimeCompare(r *http.Request, _ []byte) (any, int, error) {
	e := w.runtimeEnv(r.Context())
	st, err := e.store(w.root)
	if err != nil {
		return nil, http.StatusServiceUnavailable, err
	}
	q := r.URL.Query()
	var as [2]*report.Analysis
	for i, id := range []string{q.Get("a"), q.Get("b")} {
		if id == "" {
			return nil, http.StatusBadRequest, errors.New("informe as capturas a e b")
		}
		if as[i], err = w.analyseCapture(e, st, id); err != nil {
			return nil, http.StatusNotFound, err
		}
	}
	s := export.NewRuntimeSection(as[1], report.Compare(as[0], as[1]))
	if list, err := st.List(); err == nil {
		s.Captures = list
	}
	return s, 0, nil
}

// ---------- CodeAudit API (Security view) ----------

// current returns the analysed audit, or an error while there is none.
func (w *workspace) current() (*repoAnalysis, int, error) {
	w.mu.Lock()
	r := w.repo
	w.mu.Unlock()
	switch {
	case r == nil:
		return nil, http.StatusConflict, errors.New("the analysis is not ready yet")
	case r.a == nil:
		return nil, http.StatusConflict, fmt.Errorf("CodeAudit: %v", r.auditErr)
	}
	return r, 0, nil
}

func (w *workspace) aiStatus(r *http.Request, _ []byte) (any, int, error) {
	if w.invErr != nil {
		return ai.Status{Error: w.invErr.Error()}, 0, nil
	}
	return w.inv.Client.Probe(r.Context()), 0, nil
}

func (w *workspace) explain(r *http.Request, body []byte) (any, int, error) {
	var req struct {
		Finding string `json:"finding"`
		Model   string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, http.StatusBadRequest, err
	}
	if w.invErr != nil {
		return nil, http.StatusServiceUnavailable, w.invErr
	}
	ra, code, err := w.current()
	if err != nil {
		return nil, code, err
	}
	fd := ra.a.Finding(req.Finding)
	if fd == nil {
		return nil, http.StatusNotFound, errors.New("unknown finding")
	}
	model := req.Model
	if model == "" {
		model = w.f.ai.model
	}
	res, err := w.inv.ExplainFinding(r.Context(), ai.InvestigationRequest{Finding: fd, Model: model})
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	return res, 0, nil
}

func (w *workspace) explainPath(r *http.Request, body []byte) (any, int, error) {
	var req struct {
		From, To, Model string
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, http.StatusBadRequest, err
	}
	if w.invErr != nil {
		return nil, http.StatusServiceUnavailable, w.invErr
	}
	ra, code, err := w.current()
	if err != nil {
		return nil, code, err
	}
	p, err := audit.PathBetween(ra.g, ra.a, req.From, req.To)
	if err != nil {
		return nil, http.StatusUnprocessableEntity, err
	}
	model := req.Model
	if model == "" {
		model = w.f.ai.model
	}
	res, err := w.inv.ExplainPath(r.Context(), pathRequest(p, req.From, req.To, model))
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	return res, 0, nil
}

func (w *workspace) review(r *http.Request, body []byte) (any, int, error) {
	var req struct {
		Finding, Status, Note string
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, http.StatusBadRequest, err
	}
	ra, code, err := w.current()
	if err != nil {
		return nil, code, err
	}
	fd := ra.a.Finding(req.Finding)
	if fd == nil {
		return nil, http.StatusNotFound, errors.New("unknown finding")
	}
	w.reviews.Lock()
	defer w.reviews.Unlock()
	reviews, err := w.store.Reviews()
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	rec, err := reviews.Decide(fd, req.Status, req.Note, ra.a.Commit, time.Now())
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	if err := w.store.SaveReviews(reviews); err != nil {
		return nil, http.StatusInternalServerError, err
	}
	return map[string]any{"record": rec, "revalidate": reviews.Revalidation(fd)}, 0, nil
}
