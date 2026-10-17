package main

// Go Deep Inspector in the TUI: the Code Flow screen gains five more tabs
// (Data Flow, Execution Trace, Branch Explorer, Performance, SQL Explorer)
// that share the symbol chosen with Find Function or "Set as root".
// Static views are computed off the UI goroutine; anything that runs code
// (tests, benchmarks, builds, EXPLAIN) waits for an explicit confirmation
// and can be cancelled with Esc.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/inspect"
	"github.com/athos/gitview/internal/inspect/branch"
	"github.com/athos/gitview/internal/inspect/dataflow"
	"github.com/athos/gitview/internal/inspect/perf"
	"github.com/athos/gitview/internal/inspect/pgexplain"
	"github.com/athos/gitview/internal/inspect/run"
	"github.com/athos/gitview/internal/inspect/sqlops"
	"github.com/athos/gitview/internal/inspect/xtrace"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	tabCodeFlow = iota
	tabDataFlow
	tabTrace
	tabBranches
	tabPerf
	tabSQL
)

var tabNames = []string{"Code Flow", "Data Flow", "Execution Trace", "Branch Explorer", "Performance", "SQL Explorer"}

const inspRunTimeout = 10 * time.Minute

type inspUI struct {
	tab     int
	lastTab int
	sess    *inspect.Session

	doc     doc
	docKey  string
	sel     int
	off     int
	loading bool
	seq     int
	msg     string
	msgErr  bool

	dfVar   string // name@line
	dfSym   string // symbol dfVar belongs to
	dfDir   dataflow.Direction
	dfDepth int
	dfHide  bool
	picking bool
	vars    []dataflow.VarInfo
	vsel    int

	showPaths bool
	perfMem   bool
	flame     bool
	trFrom    int64
	trTo      int64
	trAll     bool
	reachable bool

	confirm    *pendingRun
	confirmOff int
	cancel     context.CancelFunc
	running    string
	input      textinput.Model
	inputOn    bool
}

// tabOpts are the view options a tab is computed with (a snapshot, so the
// background computation never reads UI state).
type tabOpts struct {
	dfVar     string
	dfDir     dataflow.Direction
	dfDepth   int
	dfHide    bool
	showPaths bool
	perfMem   bool
	flame     bool
	trFrom    int64
	trTo      int64
	trAll     bool
	reachable bool
}

func (i *inspUI) opts() tabOpts {
	return tabOpts{i.dfVar, i.dfDir, i.dfDepth, i.dfHide, i.showPaths, i.perfMem, i.flame, i.trFrom, i.trTo, i.trAll, i.reachable}
}

type pendingRun struct {
	title string
	lines []string
	run   func(ctx context.Context) inspRunMsg
}

type inspDocMsg struct {
	seq int
	key string
	doc doc
	err error
}

type inspRunMsg struct {
	label string
	err   error
	apply func(m *model)
}

func newInspUI() *inspUI {
	ti := textinput.New()
	ti.Prompt = "  Arquivo: "
	ti.PromptStyle = stAccent
	ti.TextStyle = stText
	ti.CharLimit = 4096
	return &inspUI{lastTab: tabBranches, dfDir: dataflow.Both, dfDepth: 3, reachable: true, input: ti}
}

// openInspect opens the Deep Inspector on its last tab; the symbol is
// chosen with Find Function when none is selected yet.
func (m *model) openInspect() tea.Cmd {
	m.insp.tab = m.insp.lastTab
	if m.flow.graph != nil && m.inspSymbol() != nil && !m.flow.loading {
		m.screen = screenFlow
		m.flow.phase = phaseGraph
		m.flow.input.Blur()
		return m.inspRefresh(false)
	}
	return m.openFlow()
}

func (m *model) inspSession() *inspect.Session {
	if m.insp.sess == nil {
		m.insp.sess = inspect.NewSession(m.repo.Root, m.flow.progs)
	} else {
		m.insp.sess.SetPrograms(m.flow.progs)
	}
	return m.insp.sess
}

// inspSymbol returns the shared symbol (the Code Flow root).
func (m *model) inspSymbol() *inspect.Resolved {
	f := m.flow
	if f.prog == nil || f.rootID == "" {
		return nil
	}
	fn := f.prog.FuncByID(f.rootID)
	if fn == nil {
		return nil
	}
	return &inspect.Resolved{Prog: f.prog, Fn: fn, Ref: f.prog.Ref(fn)}
}

func (m *model) inspKeyFor() string {
	i := m.insp
	return fmt.Sprintf("%s|%d|%s|%s|%d|%v|%v|%v|%v|%d|%d|%v|%v|%d", m.flow.rootID, i.tab, i.dfVar, i.dfDir, i.dfDepth, i.dfHide,
		i.showPaths, i.perfMem, i.flame, i.trFrom, i.trTo, i.trAll, i.reachable, m.width)
}

// inspRefresh recomputes the current tab in the background.
func (m *model) inspRefresh(force bool) tea.Cmd {
	i := m.insp
	if i.tab == tabCodeFlow {
		return nil
	}
	r := m.inspSymbol()
	key := m.inspKeyFor()
	if !force && key == i.docKey && i.doc != nil {
		return nil
	}
	i.seq++
	seq := i.seq
	i.loading = true
	if r == nil {
		i.loading = false
		i.doc = doc{{spans: []tspan{spS(inspect.NoSymbol, sMuted)}}}
		i.docKey = key
		return nil
	}
	sess := m.inspSession()
	tab, width := i.tab, m.width
	if tab == tabDataFlow && (i.dfVar == "" || i.dfSym != r.Ref.ID) {
		i.vars = dataflow.Variables(r.Prog, r.Fn)
		i.dfVar, i.dfSym = defaultVariable(r, i.vars), r.Ref.ID
		key = m.inspKeyFor()
	}
	opts := i.opts()
	return func() tea.Msg {
		d, err := buildTabDoc(sess, r, tab, opts, width)
		return inspDocMsg{seq: seq, key: key, doc: d, err: err}
	}
}

// defaultVariable picks a non-receiver, non-context parameter, or the first local.
func defaultVariable(r *inspect.Resolved, vars []dataflow.VarInfo) string {
	recv := r.Fn.Signature.Recv() != nil
	for i, v := range vars {
		if v.Kind == "param" && !(recv && i == 0) && v.Type != "context.Context" {
			return fmt.Sprintf("%s@%d", v.Name, v.Line)
		}
	}
	for _, v := range vars {
		if v.Kind == "local" {
			return fmt.Sprintf("%s@%d", v.Name, v.Line)
		}
	}
	if len(vars) > 0 {
		return fmt.Sprintf("%s@%d", vars[0].Name, vars[0].Line)
	}
	return ""
}

func splitVar(v string) (string, int) {
	name, line, _ := strings.Cut(v, "@")
	n, _ := strconv.Atoi(line)
	return name, n
}

func buildTabDoc(sess *inspect.Session, r *inspect.Resolved, tab int, o tabOpts, width int) (doc, error) {
	switch tab {
	case tabDataFlow:
		name, line := splitVar(o.dfVar)
		if name == "" {
			return doc{{spans: []tspan{spS("Esta função não tem variáveis para rastrear.", sMuted)}}}, nil
		}
		res, err := sess.DataFlow(r, dataflow.Query{Variable: name, Line: line, Return: -1}, dataflow.Options{Direction: o.dfDir, MaxDepth: o.dfDepth, MaxNodes: 300})
		if err != nil {
			return nil, err
		}
		return renderDataFlow(res, o.dfHide), nil
	case tabBranches:
		g, err := sess.Branches(r)
		if err != nil {
			return nil, err
		}
		d := renderBranches(g, o.showPaths)
		if g.Coverage == nil {
			d = append(doc{{spans: []tspan{spS(inspect.NoCoverage+" · c executa os testes do pacote com cobertura · I importa um coverprofile", sMuted)}}}, d...)
		}
		return d, nil
	case tabSQL:
		return renderSQL(sess.SQL(r, o.reachable, 4), sess.Snapshot().Plans), nil
	case tabPerf:
		return perfDoc(sess.Snapshot(), r, o), nil
	case tabTrace:
		tr := sess.Snapshot().Trace
		if tr == nil {
			return doc{
				{spans: []tspan{spS(inspect.NoTrace, sMuted)}},
				{},
				{spans: []tspan{spS("t", sBold), spS("  executa os testes do pacote gravando um trace (pede confirmação)", sSubtle)}},
				{spans: []tspan{spS("T", sBold), spS("  idem, medindo esta função com uma região temporária (go test -overlay)", sSubtle)}},
				{spans: []tspan{spS("I", sBold), spS("  importa um arquivo de trace (go test -trace, runtime/trace)", sSubtle)}},
			}, nil
		}
		return renderTrace(tr, r.Ref.ID, max(width-4, 40), !o.trAll, o.trFrom, o.trTo), nil
	}
	return nil, nil
}

func perfDoc(rt inspect.Runtime, r *inspect.Resolved, o tabOpts) doc {
	var d doc
	cpu, mem, esc := rt.CPU, rt.Mem, rt.Escape
	if cpu == nil && mem == nil && esc == nil && len(rt.Benches) == 0 {
		return doc{
			{spans: []tspan{spS(inspect.NoProfile, sMuted)}},
			{},
			{spans: []tspan{spS("b", sBold), spS("  executa os benchmarks do pacote com -benchmem e perfis de CPU e memória (pede confirmação)", sSubtle)}},
			{spans: []tspan{spS("e", sBold), spS("  compila o pacote com -gcflags=-m=2 (diagnósticos de escape e inlining)", sSubtle)}},
			{spans: []tspan{spS("I", sBold), spS("  importa um perfil pprof ou uma saída de go test -bench", sSubtle)}},
		}
	}
	rep := cpu
	if o.perfMem || rep == nil {
		rep = mem
		if rep == nil {
			rep = cpu
		}
	}
	if rep != nil {
		d = append(d, renderProfile(rep, 25, r.Ref.ID)...)
		if o.flame {
			d.blank()
			d = append(d, renderFlame(rep, 10, 2)...)
		}
	}
	if n := len(rt.Benches); n > 0 {
		d.blank()
		d = append(d, renderBench(rt.Benches[n-1])...)
		if n >= 2 {
			d.blank()
			d = append(d, renderCompare(perf.Compare(rt.Benches[n-2], rt.Benches[n-1]))...)
		}
	}
	if esc != nil {
		d.blank()
		d = append(d, renderEscape(esc, r.Ref.ID, 0)...)
	}
	return d
}

func (m *model) inspDocDone(msg inspDocMsg) {
	i := m.insp
	if msg.seq != i.seq {
		return
	}
	i.loading = false
	if msg.err != nil {
		i.doc = doc{{spans: []tspan{spS(msg.err.Error(), sBad)}}}
		var amb *dataflow.AmbiguousError
		if errors.As(msg.err, &amb) {
			i.doc = append(i.doc, docLine{spans: []tspan{spS("Use v para escolher a variável exata.", sMuted)}})
		}
	} else {
		i.doc = msg.doc
	}
	i.docKey = msg.key
	i.sel = min(i.sel, max(len(i.doc)-1, 0))
	if !i.selectable(i.sel) {
		i.sel = i.step(-1, 0)
	}
}

func (i *inspUI) selectable(k int) bool {
	return k >= 0 && k < len(i.doc) && (i.doc[k].ref != "" || i.doc[k].loc != "")
}

// step moves the selection to the next selectable line in dir.
func (i *inspUI) step(dir, from int) int {
	for k := from + dir; k >= 0 && k < len(i.doc); k += dir {
		if i.selectable(k) {
			return k
		}
	}
	if dir < 0 {
		for k := 0; k < len(i.doc); k++ {
			if i.selectable(k) {
				return k
			}
		}
	}
	return from
}

// inspStop cancels an inspector run and kills its processes before exit.
func (m *model) inspStop() {
	if m.insp.cancel != nil {
		m.insp.cancel()
	}
	run.KillAll()
}

// modal reports whether a confirmation, a run, an input or a picker owns
// the keyboard (tab keys must not bypass them).
func (i *inspUI) modal() bool {
	return i.cancel != nil || i.confirm != nil || i.inputOn || i.picking
}

// --- keys ------------------------------------------------------------------------

// inspTabKey switches tabs; it reports whether the key was used.
func (m *model) inspTabKey(key string) (tea.Cmd, bool) {
	i := m.insp
	next := -1
	switch key {
	case "1", "2", "3", "4", "5", "6":
		next = int(key[0] - '1')
	case "]":
		next = (i.tab + 1) % len(tabNames)
	case "[":
		next = (i.tab + len(tabNames) - 1) % len(tabNames)
	}
	if next < 0 {
		return nil, false
	}
	i.tab = next
	if next != tabCodeFlow {
		i.lastTab = next
	}
	i.sel, i.off, i.msg = 0, 0, ""
	return m.inspRefresh(false), true
}

func (m *model) inspKey(msg tea.KeyMsg) tea.Cmd {
	i := m.insp
	key := msg.String()
	if i.cancel != nil {
		if key == "esc" {
			i.cancel()
			i.msg = "Cancelando…"
		}
		return nil
	}
	if i.confirm != nil {
		switch key {
		case "up", "k":
			i.confirmOff = max(i.confirmOff-1, 0)
			return nil
		case "down", "j":
			i.confirmOff++
			return nil
		case "enter":
			p := i.confirm
			i.confirm = nil
			ctx, cancel := context.WithTimeout(context.Background(), inspRunTimeout)
			i.cancel, i.running = cancel, p.title
			return func() tea.Msg {
				defer cancel()
				return p.run(ctx)
			}
		case "esc", "n":
			i.confirm = nil
			i.msg, i.msgErr = "Cancelado: nada foi executado.", false
		}
		return nil
	}
	if i.inputOn {
		switch key {
		case "esc":
			i.inputOn = false
			i.input.Blur()
			return nil
		case "enter":
			i.inputOn = false
			i.input.Blur()
			return m.inspImport(strings.TrimSpace(i.input.Value()))
		}
		var cmd tea.Cmd
		i.input, cmd = i.input.Update(msg)
		return cmd
	}
	if i.picking {
		switch key {
		case "esc", "v":
			i.picking = false
		case "up", "k":
			i.vsel = max(i.vsel-1, 0)
		case "down", "j":
			i.vsel = min(i.vsel+1, max(len(i.vars)-1, 0))
		case "enter":
			if i.vsel < len(i.vars) {
				v := i.vars[i.vsel]
				i.dfVar = fmt.Sprintf("%s@%d", v.Name, v.Line)
			}
			i.picking = false
			i.sel, i.off = 0, 0
			return m.inspRefresh(false)
		}
		return nil
	}
	if cmd, ok := m.inspTabKey(key); ok {
		return cmd
	}
	r := m.inspSymbol()
	switch key {
	case "esc", "q":
		m.leaveInspector()
		return nil
	case "?":
		m.showHelp = true
		return nil
	case "/":
		f := m.flow
		f.phase = phasePick
		f.input.SetValue("")
		f.input.Width = max(m.width-22, 20)
		f.input.Focus()
		m.flowCandidates()
		return textinput.Blink
	case "up", "k":
		i.sel = i.step(-1, i.sel)
		return nil
	case "down", "j":
		i.sel = i.step(1, i.sel)
		return nil
	case "pgdown", "ctrl+d":
		i.sel = min(i.sel+m.vp.Height/2, max(len(i.doc)-1, 0))
		if !i.selectable(i.sel) {
			i.sel = i.step(1, i.sel)
		}
		return nil
	case "pgup", "ctrl+u":
		i.sel = max(i.sel-m.vp.Height/2, 0)
		if !i.selectable(i.sel) {
			i.sel = i.step(-1, i.sel)
		}
		return nil
	case "g":
		if i.sel < len(i.doc) {
			if file, line := splitLoc(i.doc[i.sel].loc); file != "" {
				if !m.gotoLine(file, line, line) {
					i.msg, i.msgErr = fmt.Sprintf("%s:%d não foi modificado no working tree", file, line), true
				}
			}
		}
		return nil
	case "R":
		return m.inspRefresh(true)
	case "enter":
		if i.sel < len(i.doc) {
			if id, ok := strings.CutPrefix(i.doc[i.sel].ref, "sym:"); ok && r != nil {
				for _, p := range m.flow.progs {
					if p.FuncByID(id) != nil {
						m.flowQuery(p, id, true)
						return m.inspRefresh(false)
					}
				}
			}
		}
		return nil
	case "I":
		i.inputOn = true
		i.input.SetValue("")
		i.input.Width = max(m.width-14, 20)
		i.input.Focus()
		return textinput.Blink
	}
	if r == nil {
		return nil
	}
	switch i.tab {
	case tabDataFlow:
		switch key {
		case "v":
			i.vars = dataflow.Variables(r.Prog, r.Fn)
			i.picking, i.vsel = true, 0
			for k, v := range i.vars {
				if fmt.Sprintf("%s@%d", v.Name, v.Line) == i.dfVar {
					i.vsel = k
				}
			}
		case "o":
			i.dfDir = dataflow.Origin
		case "d":
			i.dfDir = dataflow.Destination
		case "b":
			i.dfDir = dataflow.Both
		case "+", "=":
			i.dfDepth = min(i.dfDepth+1, 8)
		case "-":
			i.dfDepth = max(i.dfDepth-1, 1)
		case "h":
			i.dfHide = !i.dfHide
		default:
			return nil
		}
		return m.inspRefresh(false)
	case tabBranches:
		switch key {
		case "p":
			i.showPaths = !i.showPaths
			return m.inspRefresh(false)
		case "c":
			m.inspConfirmCoverage(r)
		}
	case tabPerf:
		switch key {
		case "m":
			i.perfMem = !i.perfMem
			return m.inspRefresh(false)
		case "F":
			i.flame = !i.flame
			return m.inspRefresh(false)
		case "b":
			m.inspConfirmBench(r)
		case "e":
			m.inspConfirmEscape(r)
		}
	case tabTrace:
		t := m.inspSession().Snapshot().Trace
		switch key {
		case "t", "T":
			m.inspConfirmTrace(r, key == "T")
			return nil
		case "a":
			i.trAll = !i.trAll
		case "+", "=", "-", "left", "right", "h", "l", "0":
			if t == nil {
				return nil
			}
			from, to := i.trFrom, i.trTo
			if to <= from {
				from, to = 0, t.DurationNs
			}
			span := to - from
			switch key {
			case "+", "=":
				from, to = from+span/4, to-span/4
			case "-":
				from, to = max(from-span/2, 0), min(to+span/2, t.DurationNs)
			case "left", "h":
				shift := min(span/4, from)
				from, to = from-shift, to-shift
			case "right", "l":
				shift := min(span/4, t.DurationNs-to)
				from, to = from+shift, to+shift
			case "0":
				from, to = 0, 0
			}
			if to-from < 1000 && to != 0 {
				return nil
			}
			i.trFrom, i.trTo = from, to
		default:
			return nil
		}
		return m.inspRefresh(false)
	case tabSQL:
		switch key {
		case "r":
			i.reachable = !i.reachable
			return m.inspRefresh(false)
		case "x", "X":
			m.inspConfirmExplain(r, key == "X")
		}
	}
	return nil
}

func splitLoc(l string) (string, int) {
	i := strings.LastIndexByte(l, ':')
	if i <= 0 {
		return "", 0
	}
	n, err := strconv.Atoi(l[i+1:])
	if err != nil {
		return "", 0
	}
	return l[:i], n
}

// --- actions that run code ------------------------------------------------------

func (m *model) relDir(dir string) string {
	if rel, err := filepath.Rel(m.repo.Root, dir); err == nil {
		return rel
	}
	return dir
}

func (m *model) inspConfirmCoverage(r *inspect.Resolved) {
	dir := run.ArtifactPath(m.repo.Root, "coverage")
	file := filepath.Join(dir, "cover.out")
	spec := run.Spec{Args: branch.CoverArgs(file, pkgPattern(r)), Dir: r.Prog.Dir, Timeout: inspRunTimeout}
	sess := m.inspSession()
	m.insp.confirmOff = 0
	m.insp.confirm = &pendingRun{
		title: "Os testes do pacote serão executados com cobertura (código do repositório, possíveis efeitos externos):",
		lines: []string{"$ " + spec.CommandLine(), "em " + m.relDir(spec.Dir)},
		run: func(ctx context.Context) inspRunMsg {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return inspRunMsg{err: err}
			}
			res := run.Run(ctx, spec)
			if res.Err != nil {
				return inspRunMsg{label: "cobertura", err: res.Err}
			}
			pr, err := branch.LoadProfile(file)
			if err != nil {
				return inspRunMsg{label: "cobertura", err: fmt.Errorf("go test terminou com código %d: %w", res.ExitCode, err)}
			}
			return inspRunMsg{label: fmt.Sprintf("cobertura gravada em %s (go test: código %d)", file, res.ExitCode), apply: func(m *model) {
				sess.Update(func(s *inspect.Session) { s.Coverage = pr })
			}}
		},
	}
}

func (m *model) inspConfirmBench(r *inspect.Resolved) {
	dir := run.ArtifactPath(m.repo.Root, "bench")
	opt := perf.BenchOptions{Bench: ".", Count: 6, Profiles: true, OutDir: dir}
	spec, _ := perf.BenchSpec(r.Prog.Dir, pkgPattern(r), opt)
	sess := m.inspSession()
	progs := m.flow.progs
	modDir, pkg := r.Prog.Dir, pkgPattern(r)
	m.insp.confirmOff = 0
	m.insp.confirm = &pendingRun{
		title: "Os benchmarks do pacote serão executados (código do repositório):",
		lines: []string{"$ " + spec.CommandLine(), "em " + m.relDir(spec.Dir), "resultados em " + dir},
		run: func(ctx context.Context) inspRunMsg {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return inspRunMsg{err: err}
			}
			c := perf.RunBench(ctx, progs, modDir, pkg, opt, inspRunTimeout)
			if c.Run == nil {
				return inspRunMsg{label: "benchmarks", err: errors.New(strings.Join(c.Errors, "; "))}
			}
			return inspRunMsg{label: "benchmarks e perfis gravados em " + dir, apply: func(m *model) {
				sess.AddBench(c.Run)
				sess.Update(func(s *inspect.Session) {
					if c.CPU != nil {
						s.CPU = c.CPU
					}
					if c.Mem != nil {
						s.Mem = c.Mem
					}
				})
			}}
		},
	}
}

func (m *model) inspConfirmEscape(r *inspect.Resolved) {
	spec := perf.EscapeSpec(r.Prog.Dir, pkgPattern(r))
	spec.Timeout = inspRunTimeout
	sess := m.inspSession()
	progs := m.flow.progs
	m.insp.confirmOff = 0
	m.insp.confirm = &pendingRun{
		title: "O pacote será compilado com diagnósticos do compilador (o binário é descartado; nada é executado):",
		lines: []string{"$ " + spec.CommandLine(), "em " + m.relDir(spec.Dir)},
		run: func(ctx context.Context) inspRunMsg {
			res := run.Run(ctx, spec)
			if res.Err != nil {
				return inspRunMsg{label: "build", err: res.Err}
			}
			rep := perf.ParseEscape(res.Stderr, progs)
			rep.Artifact = evidence.Artifact{Kind: "escape", Name: "go build -gcflags=-m=2", Command: spec.CommandLine(), CapturedAt: time.Now()}
			if res.ExitCode != 0 {
				rep.BuildFailed()
			}
			label := fmt.Sprintf("%d diagnósticos do compilador", len(rep.Diags))
			if res.ExitCode != 0 {
				label += fmt.Sprintf(" (go build: código %d)", res.ExitCode)
			}
			return inspRunMsg{label: label, apply: func(m *model) { sess.Update(func(s *inspect.Session) { s.Escape = rep }) }}
		},
	}
}

func (m *model) inspConfirmTrace(r *inspect.Resolved, instrument bool) {
	dir := run.ArtifactPath(m.repo.Root, "trace")
	var ids []string
	if instrument {
		ids = []string{r.Ref.ID}
	}
	pl, err := xtrace.PlanCapture(r.Prog, xtrace.CaptureOptions{Pkg: pkgPattern(r), Instrument: ids, OutDir: dir, Timeout: inspRunTimeout})
	if err != nil {
		m.insp.msg, m.insp.msgErr = err.Error(), true
		return
	}
	lines := []string{"$ " + pl.Spec.CommandLine(), "em " + m.relDir(pl.Spec.Dir)}
	for _, id := range pl.Instrumented {
		lines = append(lines, "região temporária em "+id+" (cópia via -overlay; arquivos do projeto não são alterados)")
	}
	for _, s := range pl.Skipped {
		lines = append(lines, "não instrumentada: "+s)
	}
	sess := m.inspSession()
	progs := m.flow.progs
	m.insp.confirmOff = 0
	m.insp.confirm = &pendingRun{
		title: "Os testes do pacote serão executados com trace (código do repositório, possíveis efeitos externos):",
		lines: lines,
		run: func(ctx context.Context) inspRunMsg {
			c := xtrace.RunCapture(ctx, pl, progs)
			if c.Err != nil {
				return inspRunMsg{label: "trace", err: c.Err}
			}
			return inspRunMsg{label: "trace gravado em " + pl.TraceFile, apply: func(m *model) {
				sess.Update(func(s *inspect.Session) { s.Trace = c.Timeline })
				m.insp.trFrom, m.insp.trTo = 0, 0
			}}
		},
	}
}

func (m *model) inspConfirmExplain(r *inspect.Resolved, analyze bool) {
	i := m.insp
	dsn := os.Getenv(pgexplain.EnvDSN)
	if dsn == "" {
		i.msg, i.msgErr = "EXPLAIN precisa de uma conexão configurada explicitamente: defina "+pgexplain.EnvDSN+" (servidor local de desenvolvimento).", true
		return
	}
	res := m.inspSession().SQL(r, i.reachable, 4)
	var op *sqlops.Op
	if k := i.selectedOp(); k >= 0 && k < len(res.Ops) {
		op = res.Ops[k]
	} else {
		for _, o := range res.Ops {
			if o.SQL.Text != "" {
				if op != nil {
					i.msg, i.msgErr = "Selecione a operação a explicar (↑↓).", true
					return
				}
				op = o
			}
		}
	}
	if op == nil || op.SQL.Text == "" {
		i.msg, i.msgErr = inspect.SQLUnresolved+": não há texto completo para explicar.", true
		return
	}
	req := pgexplain.Request{SQL: op.SQL.Text, Analyze: analyze}
	kind, err := pgexplain.Check(req)
	if analyze && err == nil && !sqlops.ReadOnly(kind) {
		err = errors.New("EXPLAIN ANALYZE de instruções que alteram dados só pela CLI, com --analyze --allow-write")
	}
	if err != nil {
		i.msg, i.msgErr = err.Error(), true
		return
	}
	target, local, err := pgexplain.Target(dsn)
	if err != nil {
		i.msg, i.msgErr = err.Error(), true
		return
	}
	if !local {
		i.msg, i.msgErr = target+" não é um servidor local; servidores remotos só pela CLI com --allow-remote.", true
		return
	}
	mode := "EXPLAIN: a instrução NÃO é executada (transação somente leitura, desfeita no fim)"
	if analyze {
		mode = "EXPLAIN ANALYZE: a instrução SERÁ EXECUTADA numa transação somente leitura, desfeita no fim; funções chamadas por ela podem ter efeitos que a transação não impede"
	}
	lines := []string{"servidor: " + target, mode}
	for _, l := range strings.Split(sqlops.Format(op.SQL.Text), "\n") {
		lines = append(lines, "    "+l)
	}
	sess := m.inspSession()
	m.insp.confirmOff = 0
	m.insp.confirm = &pendingRun{
		title: fmt.Sprintf("Plano de execução de %s %s em %s:", op.Lib, op.Op, loc(op.File, op.Line)),
		lines: lines,
		run: func(ctx context.Context) inspRunMsg {
			p, err := pgexplain.Explain(ctx, pgexplain.Config{DSN: dsn, Timeout: 15 * time.Second}, req)
			if err != nil {
				return inspRunMsg{label: "EXPLAIN", err: err}
			}
			return inspRunMsg{label: "plano obtido de " + p.Target, apply: func(m *model) { sess.AddPlan(p) }}
		},
	}
}

// selectedOp returns the index of the SQL operation the selection is on.
func (i *inspUI) selectedOp() int {
	for k := min(i.sel, len(i.doc)-1); k >= 0; k-- {
		if n, ok := strings.CutPrefix(i.doc[k].ref, "op:"); ok {
			v, _ := strconv.Atoi(n)
			return v
		}
	}
	return -1
}

func (m *model) inspRunDone(msg inspRunMsg) tea.Cmd {
	i := m.insp
	i.cancel, i.running = nil, ""
	if msg.err != nil {
		i.msg, i.msgErr = msg.label+": "+msg.err.Error(), true
		return nil
	}
	if msg.apply != nil {
		msg.apply(m)
	}
	i.msg, i.msgErr = msg.label, false
	return m.inspRefresh(true)
}

// inspImport loads a file: coverage profile, pprof profile, benchmark
// output or execution trace, recognised by content.
func (m *model) inspImport(path string) tea.Cmd {
	i := m.insp
	if path == "" {
		return nil
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	sess := m.inspSession()
	progs := m.flow.progs
	tab := i.tab
	return func() tea.Msg {
		var errs []string
		if tab == tabBranches {
			pr, err := branch.LoadProfile(path)
			if err == nil {
				return inspRunMsg{label: "cobertura importada: " + pr.Name, apply: func(m *model) { sess.Update(func(s *inspect.Session) { s.Coverage = pr }) }}
			}
			errs = append(errs, err.Error())
		}
		if tab == tabTrace {
			t, err := xtrace.Load(path, progs, xtrace.Limits{})
			if err == nil {
				return inspRunMsg{label: "trace importado: " + t.Artifact.Name, apply: func(m *model) {
					sess.Update(func(s *inspect.Session) { s.Trace = t })
					m.insp.trFrom, m.insp.trTo = 0, 0
				}}
			}
			errs = append(errs, err.Error())
		}
		if tab == tabPerf || tab == tabCodeFlow {
			if r, err := perf.Load(path, progs, perf.Options{}); err == nil {
				if r.Kind == "heap" {
					if alloc, err := perf.Load(path, progs, perf.Options{SampleType: "alloc_space"}); err == nil {
						r = alloc
					}
					return inspRunMsg{label: "perfil de memória importado: " + r.Artifact.Name, apply: func(m *model) {
						sess.Update(func(s *inspect.Session) { s.Mem = r })
						m.insp.perfMem = true
					}}
				}
				return inspRunMsg{label: "perfil importado: " + r.Artifact.Name, apply: func(m *model) {
					sess.Update(func(s *inspect.Session) { s.CPU = r })
					m.insp.perfMem = false
				}}
			} else {
				errs = append(errs, err.Error())
			}
			if b, err := perf.LoadBench(path); err == nil {
				return inspRunMsg{label: "benchmarks importados: " + b.Artifact.Name, apply: func(m *model) { sess.AddBench(b) }}
			} else {
				errs = append(errs, err.Error())
			}
		}
		return inspRunMsg{label: "importação", err: errors.New(strings.Join(errs, "; "))}
	}
}

// --- view ---------------------------------------------------------------------------

func (m *model) inspTabBar(w int) string {
	var parts []string
	for k, n := range tabNames {
		label := fmt.Sprintf("%d %s", k+1, n)
		if k == m.insp.tab {
			parts = append(parts, stTitle.Render(label))
		} else {
			parts = append(parts, stMuted.Render(label))
		}
	}
	return ansiTruncate(" "+strings.Join(parts, stBorder.Render("  │  ")), w)
}

func (m *model) inspView(w, h int) string {
	i := m.insp
	r := m.inspSymbol()
	lines := []string{m.inspTabBar(w)}
	if r != nil {
		lines = append(lines, " "+stBold.Render(r.Ref.Qualified)+"  "+stSubtle.Render(loc(r.Ref.File, r.Ref.Line)))
	} else {
		lines = append(lines, " "+stMuted.Render(inspect.NoSymbol+" — / para buscar uma função"))
	}
	lines = append(lines, "")
	var footer []string
	switch {
	case i.running != "":
		footer = append(footer, " "+stMod.Render("Executando… ")+stMuted.Render(ansiTruncate(i.running, w-16))+stMuted.Render("  (Esc cancela)"))
	case i.confirm != nil:
		// The confirmation replaces the list so the whole command and the
		// keys are visible; very long statements are paged, never cut.
		var body []string
		for _, l := range i.confirm.lines {
			for _, part := range hardWrap(l, max(w-6, 20)) {
				body = append(body, "   "+stText.Render(part))
			}
		}
		room := max(h-len(lines)-4, 3)
		i.confirmOff = max(min(i.confirmOff, len(body)-room), 0)
		lines = append(lines, " "+stMod.Render(ansiTruncate(i.confirm.title, w-2)), "")
		end := min(i.confirmOff+room, len(body))
		lines = append(lines, body[i.confirmOff:end]...)
		more := ""
		if len(body) > room {
			more = fmt.Sprintf("   ↑↓ rola (%d–%d de %d linhas)", i.confirmOff+1, end, len(body))
		}
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		lines = append(lines[:h-1], " "+stBold.Render("Enter")+stMuted.Render(" executa   ")+stBold.Render("Esc")+stMuted.Render(" cancela — nada roda sem confirmação")+stMuted.Render(more))
		return strings.Join(lines, "\n")
	case i.inputOn:
		footer = append(footer, " "+stBorder.Render(strings.Repeat("─", max(w-2, 1))), i.input.View(),
			" "+stMuted.Render("Importa: cobertura (Branch Explorer), trace (Execution Trace), perfil pprof ou saída de benchmark (Performance)"))
	case i.picking:
		footer = append(footer, " "+stBorder.Render(strings.Repeat("─", max(w-2, 1))), " "+stBold.Render("Variável a rastrear"))
		start := max(i.vsel-6, 0)
		for k := start; k < len(i.vars) && k < start+12; k++ {
			v := i.vars[k]
			line := fmt.Sprintf("%-7s %-18s %-26s linha %d", v.Kind, v.Name, v.Type, v.Line)
			if k == i.vsel {
				footer = append(footer, " "+stAccent.Render(sym.closed)+" "+stBold.Render(ansiTruncate(line, w-4)))
			} else {
				footer = append(footer, "   "+stSubtle.Render(ansiTruncate(line, w-4)))
			}
		}
	default:
		footer = m.inspDetail(w)
	}
	if i.msg != "" && i.confirm == nil {
		st := stAdd
		if i.msgErr {
			st = stDel
		}
		footer = append(footer, " "+st.Render(ansiTruncate(i.msg, w-2)))
	}
	listH := max(h-len(lines)-len(footer), 3)
	if i.loading && len(i.doc) == 0 {
		lines = append(lines, "  "+stMuted.Render("Analisando…"))
	}
	if i.sel < i.off {
		i.off = i.sel
	}
	if i.sel >= i.off+listH {
		i.off = i.sel - listH + 1
	}
	i.off = max(min(i.off, len(i.doc)-listH), 0)
	for k := i.off; k < len(i.doc) && k-i.off < listH; k++ {
		lines = append(lines, i.doc[k].styled(w, k == i.sel && i.selectable(k)))
	}
	for len(lines) < h-len(footer) {
		lines = append(lines, "")
	}
	return strings.Join(append(lines, footer...), "\n")
}

// inspDetail shows the source around the selected line's location.
func (m *model) inspDetail(w int) []string {
	i := m.insp
	if i.sel >= len(i.doc) || !i.selectable(i.sel) {
		return nil
	}
	file, line := splitLoc(i.doc[i.sel].loc)
	if file == "" {
		return nil
	}
	var src string
	for _, p := range m.flow.progs {
		if src = p.Source(file, max(line-1, 1), line+1); src != "" {
			break
		}
	}
	if src == "" {
		return nil
	}
	out := []string{" " + stBorder.Render(strings.Repeat("─", max(w-2, 1)))}
	for k, l := range strings.Split(src, "\n") {
		n := max(line-1, 1) + k
		num := stMuted.Render(fmt.Sprintf("%5d ", n))
		text := strings.ReplaceAll(l, "\t", "    ")
		if n == line {
			out = append(out, " "+num+lipgloss.NewStyle().Background(colSelBg).Inherit(stText).Render(ansiTruncate(text, w-9)))
		} else {
			out = append(out, " "+num+stSubtle.Render(ansiTruncate(text, w-9)))
		}
	}
	return out
}

func (m *model) inspFooter() string {
	i := m.insp
	hints := []keyHint{{"1-6", "Aba"}}
	switch i.tab {
	case tabDataFlow:
		hints = append(hints, keyHint{"v", "Variável"}, keyHint{"o/d/b", string(i.dfDir)}, keyHint{"+/-", fmt.Sprintf("Prof. %d", i.dfDepth)}, keyHint{"h", "Intermediários"})
	case tabBranches:
		hints = append(hints, keyHint{"p", "Caminhos"}, keyHint{"c", "Cobertura"}, keyHint{"I", "Importar"})
	case tabPerf:
		hints = append(hints, keyHint{"b", "Benchmarks"}, keyHint{"e", "Escape"}, keyHint{"m", "CPU/Mem"}, keyHint{"F", "Flame"}, keyHint{"I", "Importar"})
	case tabTrace:
		hints = append(hints, keyHint{"t/T", "Capturar"}, keyHint{"I", "Importar"}, keyHint{"+/- ←→", "Zoom"}, keyHint{"a", "Runtime"})
	case tabSQL:
		hints = append(hints, keyHint{"r", "Alcançáveis"}, keyHint{"x/X", "EXPLAIN"})
	}
	hints = append(hints, keyHint{"g", "Ir ao diff"}, keyHint{"/", "Buscar"}, keyHint{"?", "Ajuda"}, keyHint{"Esc", "Voltar"})
	return renderHints(m.width, hints, 2)
}

var inspHelp = []keyHint{
	{"1-6  [  ]", "abas: Code Flow, Data Flow, Execution Trace, Branch Explorer, Performance, SQL Explorer"},
	{"↑ ↓  PgUp PgDn", "navegar; o trecho de código da linha selecionada aparece embaixo"},
	{"g", "abrir a linha selecionada no diff, se modificada"},
	{"Enter", "Performance: inspecionar a função do ranking"},
	{"/", "buscar outra função (compartilhada por todas as abas)"},
	{"R", "recalcular"},
	{"I", "importar cobertura, trace, perfil pprof ou saída de benchmark"},
	{"Data Flow", "v variável · o origem · d destino · b ambos · +/- profundidade · h oculta intermediários"},
	{"Branch Explorer", "p caminhos · c executa testes com cobertura (confirma antes)"},
	{"Performance", "b benchmarks + perfis · e escape analysis · m CPU/memória · F flame graph"},
	{"Execution Trace", "t captura trace dos testes · T idem medindo a função · +/- ← → zoom · 0 tudo · a goroutines do runtime"},
	{"SQL Explorer", "r operações alcançáveis · x EXPLAIN · X EXPLAIN ANALYZE (só leitura; exige " + pgexplain.EnvDSN + ")"},
	{"Esc", "cancelar execução / fechar / voltar"},
}

// hardWrap splits s into lines of at most w runes, preferring spaces.
func hardWrap(s string, w int) []string {
	var out []string
	r := []rune(s)
	for len(r) > w {
		cut := w
		for k := w; k > w/2; k-- {
			if r[k] == ' ' {
				cut = k
				break
			}
		}
		out = append(out, string(r[:cut]))
		r = r[cut:]
		if len(r) > 0 && r[0] == ' ' {
			r = r[1:]
		}
		r = append([]rune("  "), r...)
	}
	return append(out, string(r))
}
