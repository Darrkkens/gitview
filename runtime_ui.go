package main

// TUI screen of the Runtime Performance Inspector (key m): process
// selector, capture options, live capture, report, history and comparison.

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect"
	"github.com/athos/gitview/internal/live/capture"
	"github.com/athos/gitview/internal/live/goprof"
	"github.com/athos/gitview/internal/live/proc"
	"github.com/athos/gitview/internal/live/report"
	"github.com/athos/gitview/internal/live/srcmap"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type rtView int

const (
	rtProcs rtView = iota
	rtSetup
	rtLive
	rtReport
	rtHistory
)

// Capture options shown in the setup view, toggled with 1–7.
const (
	optProxy = iota
	optCollector
	optCPU
	optHeap
	optTrace
	optGoroutine
	optRuntime
	optCount
)

var optNames = []string{"proxy local (correlaciona requisições)", "collector do GitView (se presente)", "perfil de CPU", "heap e alocações (GC antes/depois)", "execution trace (5 s)", "goroutines no fim", "runtime/metrics (collector)"}

var (
	rtDurations = []time.Duration{0, 10 * time.Second, 30 * time.Second, time.Minute}
	rtIntervals = []time.Duration{250 * time.Millisecond, 100 * time.Millisecond, 50 * time.Millisecond, 500 * time.Millisecond, time.Second}
)

type runtimeUI struct {
	view      rtView
	back      rtView // where Esc returns from the report
	procs     []*proc.Process
	psel      int
	listing   bool
	pidInput  textinput.Model
	typing    bool
	target    *capture.Target
	preparing bool
	opts      [optCount]bool
	dur       int
	every     int    // index in rtIntervals
	port      int    // index of the proxy target in the process's ports; -1: default
	route     string // route filter of the report ("" all)
	confirm   []string
	sess      *capture.Session
	live      capture.Live
	analyzing bool
	analysis  *report.Analysis
	cmp       *report.Comparison
	doc       doc
	sel, off  int
	history   []capture.Entry
	hsel      int
	compareTo string // capture chosen as B while picking A in the history
	opened    bool   // the Deep Inspector was opened from the report: Esc returns here
	delArmed  string
	progs     []*flow.Program
	msg       string
	msgErr    bool
}

func newRuntimeUI() *runtimeUI {
	ti := textinput.New()
	ti.Prompt = "  PID: "
	ti.PromptStyle = stAccent
	ti.TextStyle = stText
	ti.CharLimit = 10
	r := &runtimeUI{pidInput: ti, port: -1}
	r.opts[optProxy], r.opts[optCollector] = true, true
	return r
}

type (
	rtProcsMsg struct {
		list []*proc.Process
		err  error
	}
	rtTargetMsg struct {
		t   *capture.Target
		err error
	}
	rtTickMsg     struct{}
	rtDoneMsg     struct{ err error }
	rtAnalysisMsg struct {
		a     *report.Analysis
		cmp   *report.Comparison
		progs []*flow.Program
		err   error
	}
	rtHistoryMsg struct {
		list []capture.Entry
		err  error
	}
)

// setMsg shows a message; it may quote the monitored application (errors),
// so control characters are neutralised.
func (r *runtimeUI) setMsg(s string, isErr bool) { r.msg, r.msgErr = cleanText(s), isErr }

// active reports whether a capture is running.
func (r *runtimeUI) active() bool { return r.sess != nil }

func (m *model) rtStore() (*capture.Store, error) {
	e := &runtimeEnv{}
	return e.store(m.repo.Root)
}

func (m *model) openRuntime() tea.Cmd {
	m.screen = screenRuntime
	r := m.rt
	if capture.Disabled() {
		r.setMsg("Runtime Performance Inspector desativado (GITVIEW_RUNTIME=off).", true)
		return nil
	}
	if err := proc.Supported(); err != nil {
		r.setMsg(err.Error(), true)
		return nil
	}
	if r.active() {
		r.view = rtLive
		return m.rtTick()
	}
	if r.view == rtProcs && !r.listing {
		return m.rtList()
	}
	return nil
}

func (m *model) rtList() tea.Cmd {
	m.rt.listing = true
	return func() tea.Msg {
		list, err := proc.Default.List(true)
		return rtProcsMsg{list, err}
	}
}

func (m *model) rtTick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return rtTickMsg{} })
}

func (m *model) rtPrepare(pid int) tea.Cmd {
	r := m.rt
	r.preparing = true
	r.setMsg(fmt.Sprintf("Inspecionando o processo %d (/proc, collector, net/http/pprof nas portas dele)…", pid), false)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		t, err := capture.Prepare(ctx, proc.Default, pid)
		return rtTargetMsg{t, err}
	}
}

func (m *model) rtHistoryLoad() tea.Cmd {
	return func() tea.Msg {
		st, err := m.rtStore()
		if err != nil {
			return rtHistoryMsg{err: err}
		}
		list, err := st.List()
		return rtHistoryMsg{list, err}
	}
}

// rtUpdate handles the screen's messages; it reports whether msg was one.
func (m *model) rtUpdate(msg tea.Msg) (tea.Cmd, bool) {
	r := m.rt
	switch msg := msg.(type) {
	case rtProcsMsg:
		r.listing = false
		if msg.err != nil {
			r.setMsg(msg.err.Error(), true)
		}
		r.procs = msg.list
		r.psel = min(r.psel, max(len(r.procs)-1, 0))
		r.doc = renderProcList(r.procs)
	case rtTargetMsg:
		r.preparing = false
		if msg.err != nil {
			r.setMsg(msg.err.Error(), true)
			return nil, true
		}
		r.target, r.view, r.msg, r.port = msg.t, rtSetup, "", -1
		ep := msg.t.Endpoint()
		if ep == nil {
			for _, o := range []int{optCPU, optHeap, optTrace, optGoroutine, optRuntime} {
				r.opts[o] = false
			}
		}
		if msg.t.Collector == nil {
			r.opts[optRuntime] = false
		}
		r.opts[optProxy] = len(msg.t.Process.Ports) > 0
	case rtTickMsg:
		if r.sess == nil {
			return nil, true
		}
		r.live = r.sess.Live(60)
		return m.rtTick(), true
	case rtDoneMsg:
		s := r.sess
		r.sess = nil
		if msg.err != nil {
			r.view = rtSetup
			if strings.Contains(msg.err.Error(), "cancelled") {
				r.setMsg("Captura cancelada: nada foi salvo.", false)
			} else {
				r.setMsg(msg.err.Error(), true)
			}
			return nil, true
		}
		c, err := s.Wait()
		if err != nil {
			r.setMsg(err.Error(), true)
			return nil, true
		}
		r.view, r.back = rtReport, rtSetup
		return m.rtAnalyze(c, nil), true
	case rtAnalysisMsg:
		r.analyzing = false
		if msg.err != nil {
			r.setMsg(msg.err.Error(), true)
			return nil, true
		}
		if len(msg.progs) > 0 {
			r.progs = msg.progs
		}
		r.analysis, r.cmp, r.route = msg.a, msg.cmp, ""
		r.view, r.sel, r.off = rtReport, 0, 0
		m.rtRender()
		if msg.cmp != nil {
			r.setMsg("Comparação: A = "+msg.cmp.AID+" (antes), B = "+msg.cmp.BID+" (depois).", false)
		} else {
			r.setMsg("Captura "+msg.a.ID+" salva. e exporta HTML · c compara com outra captura · Enter abre a função.", false)
		}
	case rtHistoryMsg:
		if msg.err != nil {
			r.setMsg(msg.err.Error(), true)
		}
		r.history = msg.list
		r.hsel = min(r.hsel, max(len(r.history)-1, 0))
	default:
		return nil, false
	}
	return nil, true
}

// rtAnalyze builds the report of c (compared with base when given) in the
// background, relating it to the repository's code.
func (m *model) rtAnalyze(c *capture.Capture, base *capture.Capture) tea.Cmd {
	m.rt.analyzing = true
	m.rt.setMsg("Analisando a captura e relacionando com o código Go…", false)
	root, cache, known := m.repo.Root, m.flowCache, m.rt.progs
	return func() tea.Msg {
		progs := known
		if len(progs) == 0 {
			progs = loadModuleFor(cache, root, c.Process.MainModule)
		}
		var mapper *srcmap.Mapper
		if len(progs) > 0 {
			mapper = srcmap.New(root, progs)
		}
		a, err := report.Analyze(c, mapper)
		if err != nil {
			return rtAnalysisMsg{err: err}
		}
		var cmp *report.Comparison
		if base != nil {
			b, err := report.Analyze(base, mapper)
			if err != nil {
				return rtAnalysisMsg{err: err}
			}
			cmp = report.Compare(b, a)
		}
		return rtAnalysisMsg{a: a, cmp: cmp, progs: progs}
	}
}

// loadModuleFor loads the module that built the process when the
// repository has it, else every module of the repository.
func loadModuleFor(cache *flow.Cache, root, mainModule string) []*flow.Program {
	ctx, cancel := context.WithTimeout(context.Background(), flowLoadTimeout)
	defer cancel()
	mods := flow.FindModules(root)
	for _, d := range mods {
		if mainModule != "" && modulePath(d) == mainModule {
			mods = []string{d}
			break
		}
	}
	var progs []*flow.Program
	for _, d := range mods {
		if p, _, err := cache.Get(ctx, root, d); err == nil {
			progs = append(progs, p)
		}
	}
	return progs
}

func (m *model) rtRender() {
	r := m.rt
	switch {
	case r.cmp != nil:
		r.doc = renderComparison(r.cmp)
	case r.analysis != nil:
		r.doc = renderAnalysis(r.analysis, r.route, m.width)
	}
}

// rtStart starts the capture with the chosen options.
func (m *model) rtConfig() capture.Config {
	r := m.rt
	pl := goprof.Plan{CPU: r.opts[optCPU], Heap: r.opts[optHeap], HeapGC: true, Trace: r.opts[optTrace], Goroutine: r.opts[optGoroutine], Runtime: r.opts[optRuntime]}
	if !pl.Empty() {
		pl.Endpoint = r.target.EndpointFor(r.opts[optCollector])
	}
	cfg := capture.Config{Duration: rtDurations[r.dur], Interval: rtIntervals[r.every], Proxy: r.opts[optProxy], Collector: r.opts[optCollector], Profile: pl, Repo: m.repo.Root}
	if ports := r.target.Process.Ports; r.port >= 0 && r.port < len(ports) {
		cfg.Target = ports[r.port].Dial()
	}
	return cfg
}

func (m *model) rtStart() tea.Cmd {
	r := m.rt
	st, err := m.rtStore()
	if err != nil {
		r.setMsg(err.Error(), true)
		return nil
	}
	s, err := capture.Start(context.Background(), st, r.target, m.rtConfig())
	if err != nil {
		r.setMsg(err.Error(), true)
		return nil
	}
	r.sess, r.view, r.confirm = s, rtLive, nil
	r.live = s.Live(60)
	r.setMsg("", false)
	done := s.Done()
	return tea.Batch(m.rtTick(), func() tea.Msg {
		<-done
		_, err := s.Wait()
		return rtDoneMsg{err}
	})
}

// rtStopAll cancels a running capture when GitView quits; the collector is
// disarmed and the proxy closed before returning (or after 5 s).
func (m *model) rtStopAll() {
	if s := m.rt.sess; s != nil {
		s.Cancel()
		select {
		case <-s.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

// --- keys --------------------------------------------------------------------------

func (m *model) runtimeKey(msg tea.KeyMsg) tea.Cmd {
	r := m.rt
	key := msg.String()
	if r.typing {
		switch key {
		case "esc":
			r.typing = false
			r.pidInput.Blur()
		case "enter":
			r.typing = false
			r.pidInput.Blur()
			pid, err := strconv.Atoi(strings.TrimSpace(r.pidInput.Value()))
			if err != nil || pid <= 0 {
				r.setMsg("PID inválido", true)
				return nil
			}
			return m.rtPrepare(pid)
		default:
			var cmd tea.Cmd
			r.pidInput, cmd = r.pidInput.Update(msg)
			return cmd
		}
		return nil
	}
	if r.confirm != nil {
		switch key {
		case "enter":
			return m.rtStart()
		case "esc", "n":
			r.confirm = nil
			r.setMsg("Cancelado: nada foi executado.", false)
		}
		return nil
	}
	switch r.view {
	case rtProcs:
		switch key {
		case "esc", "q":
			m.screen = screenDiff
		case "up", "k":
			r.psel = max(r.psel-1, 0)
		case "down", "j":
			r.psel = min(r.psel+1, max(len(r.procs)-1, 0))
		case "r":
			return m.rtList()
		case "p":
			r.typing = true
			r.pidInput.SetValue("")
			r.pidInput.Focus()
			return textinput.Blink
		case "h":
			r.view = rtHistory
			return m.rtHistoryLoad()
		case "enter":
			if r.psel < len(r.procs) && !r.preparing {
				return m.rtPrepare(r.procs[r.psel].PID)
			}
		}
	case rtSetup:
		switch key {
		case "esc":
			r.view = rtProcs
			return m.rtList()
		case "1", "2", "3", "4", "5", "6", "7":
			i := int(key[0] - '1')
			if i >= optCPU && r.target.EndpointFor(r.opts[optCollector]) == nil {
				r.setMsg("No profiling data available: o processo não serve net/http/pprof nem o collector do GitView.", true)
				return nil
			}
			if i == optRuntime && r.target.Collector == nil {
				r.setMsg("runtime/metrics exige o collector do GitView na aplicação.", true)
				return nil
			}
			r.opts[i] = !r.opts[i]
		case "d":
			r.dur = (r.dur + 1) % len(rtDurations)
		case "f":
			r.every = (r.every + 1) % len(rtIntervals)
		case "t":
			if n := len(r.target.Process.Ports); n > 1 {
				r.port = (r.port+2)%(n+1) - 1
			}
		case "r":
			return m.rtPrepare(r.target.Process.PID)
		case "h":
			r.view = rtHistory
			return m.rtHistoryLoad()
		case "enter", "s":
			cfg := m.rtConfig()
			if !cfg.Profile.Empty() {
				r.confirm = renderCapturePlan(r.target, cfg)
				return nil
			}
			return m.rtStart()
		}
	case rtLive:
		switch key {
		case " ":
			if r.sess.Pause() {
				r.setMsg("Captura pausada: requisições e amostras deste intervalo ficam fora da análise.", false)
			} else if r.sess.Resume() {
				r.setMsg("Captura retomada.", false)
			}
		case "enter", "s":
			r.sess.Stop()
			r.setMsg("Encerrando a fase ativa (pós-captura e coleta final dos perfis)…", false)
		case "x":
			r.sess.Cancel()
		case "esc":
			m.screen = screenDiff
		}
	case rtReport:
		switch key {
		case "esc":
			if r.cmp != nil {
				r.cmp = nil
				m.rtRender()
				return nil
			}
			r.view = r.back
			if r.view == rtHistory {
				return m.rtHistoryLoad()
			}
		case "up", "k":
			r.sel = r.step(-1)
		case "down", "j":
			r.sel = r.step(1)
		case "pgdown", "ctrl+d":
			r.off += m.vp.Height / 2
		case "pgup", "ctrl+u":
			r.off = max(r.off-m.vp.Height/2, 0)
		case "g", "home":
			r.off, r.sel = 0, 0
		case "enter":
			return m.rtOpenRef()
		case "e":
			m.rtExport()
		case "c":
			if r.analysis != nil {
				r.compareTo = r.analysis.ID
				r.view = rtHistory
				r.setMsg("Escolha a captura de referência (A, antes) para comparar com "+r.analysis.ID+".", false)
				return m.rtHistoryLoad()
			}
		case "h":
			r.compareTo = ""
			r.view = rtHistory
			return m.rtHistoryLoad()
		case "n":
			r.view = rtProcs
			return m.rtList()
		case "r":
			if a := r.analysis; a != nil && r.cmp == nil && len(a.Routes) > 0 {
				i := -1
				for k, rt := range a.Routes {
					if rt.Key == r.route {
						i = k
					}
				}
				if i+1 < len(a.Routes) {
					r.route = a.Routes[i+1].Key
					r.setMsg("Rota: "+r.route+" (r alterna, até voltar a todas)", false)
				} else {
					r.route = ""
					r.setMsg("Todas as rotas.", false)
				}
				r.sel, r.off = 0, 0
				m.rtRender()
			}
		}
	case rtHistory:
		switch key {
		case "esc":
			r.compareTo = ""
			if r.analysis != nil {
				r.view = rtReport
			} else {
				r.view = rtProcs
			}
		case "up", "k":
			r.hsel = max(r.hsel-1, 0)
			r.delArmed = ""
		case "down", "j":
			r.hsel = min(r.hsel+1, max(len(r.history)-1, 0))
			r.delArmed = ""
		case "D":
			if r.hsel < len(r.history) {
				id := r.history[r.hsel].ID
				if r.delArmed != id {
					r.delArmed = id
					r.setMsg("D de novo remove a captura "+id+" do histórico.", false)
					return nil
				}
				st, err := m.rtStore()
				if err == nil {
					err = st.Delete(id)
				}
				r.delArmed = ""
				if err != nil {
					r.setMsg(err.Error(), true)
				} else {
					r.setMsg("Captura removida.", false)
				}
				return m.rtHistoryLoad()
			}
		case "enter":
			if r.hsel >= len(r.history) {
				return nil
			}
			st, err := m.rtStore()
			if err != nil {
				r.setMsg(err.Error(), true)
				return nil
			}
			c, err := st.Load(r.history[r.hsel].ID)
			if err != nil {
				r.setMsg(err.Error(), true)
				return nil
			}
			r.back = rtHistory
			if r.compareTo != "" && r.compareTo != c.ID {
				b, err := st.Load(r.compareTo)
				r.compareTo = ""
				if err != nil {
					r.setMsg(err.Error(), true)
					return nil
				}
				return m.rtAnalyze(b, c)
			}
			return m.rtAnalyze(c, nil)
		case "n":
			r.view = rtProcs
			return m.rtList()
		}
	}
	return nil
}

// step moves the selection to the previous/next selectable line.
func (r *runtimeUI) step(dir int) int {
	for i := r.sel + dir; i >= 0 && i < len(r.doc); i += dir {
		if r.doc[i].ref != "" {
			return i
		}
	}
	return r.sel
}

// rtOpenRef opens the selected function in the Deep Inspector: Code Flow
// for functions, SQL Explorer for statements. The capture's CPU profile is
// attached to the inspector's Performance tab.
func (m *model) rtOpenRef() tea.Cmd {
	r := m.rt
	if r.sel >= len(r.doc) {
		return nil
	}
	ref := r.doc[r.sel].ref
	tab := tabCodeFlow
	id := ""
	switch {
	case strings.HasPrefix(ref, "sym:"):
		id = strings.TrimPrefix(ref, "sym:")
	case strings.HasPrefix(ref, "sql:"):
		id, tab = strings.TrimPrefix(ref, "sql:"), tabSQL
	default:
		return nil
	}
	for _, p := range r.progs {
		if p.FuncByID(id) == nil {
			continue
		}
		if len(m.flow.progs) == 0 {
			m.flow.progs = r.progs
		}
		m.flowQuery(p, id, false)
		if m.flow.graph == nil {
			r.setMsg(m.flow.msg, true)
			return nil
		}
		m.screen = screenFlow
		r.opened = true
		m.insp.tab = tab
		if tab != tabCodeFlow {
			m.insp.lastTab = tab
		}
		if a := r.analysis; a != nil && a.Profiles != nil && a.Profiles.CPU != nil {
			rep := *a.Profiles.CPU
			rep.Artifact = evidence.Artifact{Kind: "cpu", Name: "runtime capture " + a.ID, CapturedAt: a.Created}
			m.inspSession().Update(func(s *inspect.Session) { s.CPU = &rep })
		}
		return m.inspRefresh(true)
	}
	r.setMsg("No matching symbol found", true)
	return nil
}

func (m *model) rtExport() {
	r := m.rt
	if r.analysis == nil {
		return
	}
	name := "runtime-" + r.analysis.ID + ".html"
	if r.cmp != nil {
		name = "runtime-compare-" + r.cmp.AID + "-" + r.cmp.BID + ".html"
	}
	path := filepath.Join(m.export.dir, name)
	if err := writeRuntimeHTML(path, r.analysis, r.cmp); err != nil {
		r.setMsg(err.Error(), true)
		return
	}
	abs, _ := filepath.Abs(path)
	r.setMsg("Relatório HTML salvo em "+flow.DisplayPath(abs), false)
}

// leaveInspector leaves the Code Flow / Deep Inspector screen: back to the
// runtime report it was opened from, else to the diff.
func (m *model) leaveInspector() {
	if m.rt.opened {
		m.rt.opened = false
		m.screen = screenRuntime
		return
	}
	m.screen = screenDiff
}

// --- view --------------------------------------------------------------------------

func (m *model) runtimeView() string {
	r := m.rt
	w, h := m.width, m.vp.Height
	var lines []string
	title := " " + stBold.Render("Runtime Performance")
	if r.active() {
		title += "  " + stDel.Render(sym.dot+" capturando")
	}
	lines = append(lines, title, "")
	body := []string{}
	switch {
	case r.confirm != nil:
		body = append(body, " "+stBold.Render("O profiling fará estas requisições ao processo "+strconv.Itoa(r.target.Process.PID)+":"), "")
		for _, l := range r.confirm {
			body = append(body, "   "+stText.Render(l))
		}
		body = append(body, "", " "+stAccent.Render("Enter executa")+stMuted.Render(" · ")+stAccent.Render("Esc cancela"))
	case r.typing:
		body = append(body, r.pidInput.View())
	case r.view == rtProcs:
		body = m.rtProcsView(w, h-4)
	case r.view == rtSetup:
		var d doc
		d.add(0, spS("Captura", sBold), spS(fmt.Sprintf("  PID %d · %s", r.target.Process.PID, r.target.Process.Comm), sSubtle))
		for i, n := range optNames {
			mark, st := "[ ]", sMuted
			if r.opts[i] {
				mark, st = "[x]", sPlain
			}
			d.add(1, spS(fmt.Sprintf("%d ", i+1), sAccent), spS(mark+" "+n, st))
		}
		durText := "até encerrar (Enter)"
		if dd := rtDurations[r.dur]; dd > 0 {
			durText = dd.String()
		}
		d.add(1, spS("d ", sAccent), sp("duração da fase ativa: "+durText), spS("  · linha de base 2 s, pós-captura 2 s", sSubtle))
		d.add(1, spS("f ", sAccent), sp("amostragem do processo a cada "+rtIntervals[r.every].String()))
		target := r.target.DefaultTarget()
		if ports := r.target.Process.Ports; r.port >= 0 && r.port < len(ports) {
			target = ports[r.port].Dial()
		}
		if target != "" {
			d.add(1, spS("t ", sAccent), sp("aplicação atrás do proxy: "+target), spS(fmt.Sprintf("  (%d portas)", len(r.target.Process.Ports)), sSubtle))
		}
		d.blank()
		d = append(d, renderTarget(r.target)...)
		for _, l := range d {
			body = append(body, l.styled(w, false))
		}
	case r.view == rtLive:
		for _, l := range renderLive(r.live, w) {
			body = append(body, l.styled(w, false))
		}
	case r.view == rtReport:
		body = m.rtDocView(w, h-4)
	case r.view == rtHistory:
		d := renderHistory(r.history)
		selLine := r.hselLine(d)
		for i, l := range d {
			body = append(body, l.styled(w, i == selLine && l.ref != ""))
		}
		body = scrollTo(body, selLine, h-4)
	}
	lines = append(lines, body...)
	if r.analyzing || r.preparing || r.listing {
		lines = append(lines[:min(len(lines), h-2)], "", " "+stMuted.Render("aguarde…"))
	}
	if r.msg != "" {
		st := stSubtle
		if r.msgErr {
			st = stDel
		}
		lines = append(lines[:min(len(lines), h-2)], "", " "+st.Render(ansiTruncate(r.msg, w-2)))
	}
	return strings.Join(lines, "\n")
}

func (r *runtimeUI) hselLine(d doc) int {
	n := 0
	for i, l := range d {
		if strings.HasPrefix(l.ref, "cap:") {
			if n == r.hsel {
				return i
			}
			n++
		}
	}
	return 0
}

// scrollTo keeps line sel visible in a window of h lines.
func scrollTo(lines []string, sel, h int) []string {
	if h <= 0 || len(lines) <= h {
		return lines
	}
	start := max(min(sel-h/2, len(lines)-h), 0)
	return lines[start : start+h]
}

func (m *model) rtProcsView(w, h int) []string {
	r := m.rt
	var out []string
	if len(r.procs) == 0 && !r.listing {
		return []string{"  " + stMuted.Render("Nenhum processo Go deste usuário. p informa um PID."), "  " + stSubtle.Render("Processos de outros usuários podem ser inspecionados pelo PID, com menos métricas.")}
	}
	out = append(out, "  "+stSubtle.Render(fmt.Sprintf("%d processos Go deste usuário · CPU: média desde o início · Enter inspeciona · p informa um PID", len(r.procs))), "")
	selLine := 0
	for i, p := range r.procs {
		ports := []string{}
		for _, pt := range p.Ports {
			ports = append(ports, pt.String())
		}
		name := p.Comm
		if p.Exe != "" {
			name = filepathBase(p.Exe)
		}
		pointer, st := "   ", stText
		if i == r.psel {
			pointer, st = " "+stAccent.Render(sym.bar)+" ", stBold
			selLine = len(out)
		}
		out = append(out, pointer+st.Render(fmt.Sprintf("%-8d %-24s", p.PID, cleanText(name)))+stSubtle.Render(fmt.Sprintf(" %-8s CPU %5.1f%% %9s  ", strings.TrimPrefix(p.GoVersion, "go"), p.CPUAvg, fmtBytes(float64(p.RSS))))+stText.Render(strings.Join(ports, " ")))
		if p.MainModule != "" {
			out = append(out, "      "+stMuted.Render(cleanText(p.MainModule)))
		}
	}
	return scrollTo(out, selLine, h)
}

func (m *model) rtDocView(w, h int) []string {
	r := m.rt
	if r.sel >= len(r.doc) {
		r.sel = 0
	}
	// Keep the selection visible.
	if r.sel < r.off {
		r.off = r.sel
	}
	if r.sel >= r.off+h {
		r.off = r.sel - h + 1
	}
	r.off = max(min(r.off, len(r.doc)-h), 0)
	var out []string
	for i := r.off; i < len(r.doc) && i < r.off+h; i++ {
		out = append(out, r.doc[i].styled(w, i == r.sel && r.doc[i].ref != ""))
	}
	return out
}

func (m *model) runtimeFooter() string {
	r := m.rt
	var hints []keyHint
	switch {
	case r.confirm != nil:
		hints = []keyHint{{"Enter", "Executar"}, {"Esc", "Cancelar"}}
	case r.typing:
		hints = []keyHint{{"Enter", "Inspecionar"}, {"Esc", "Cancelar"}}
	case r.view == rtProcs:
		hints = []keyHint{{"Enter", "Inspecionar"}, {sym.updown, "Processo"}, {"p", "PID"}, {"r", "Atualizar"}, {"h", "Histórico"}, {"Esc", "Voltar"}}
	case r.view == rtSetup:
		hints = []keyHint{{"Enter", "Iniciar captura"}, {"1-7", "Opções"}, {"d", "Duração"}, {"f", "Amostragem"}, {"t", "Porta"}, {"h", "Histórico"}, {"Esc", "Processos"}}
	case r.view == rtLive:
		hints = []keyHint{{"Enter", "Encerrar"}, {"Espaço", "Pausar"}, {"x", "Cancelar"}, {"Esc", "Sair da tela (continua)"}}
	case r.view == rtReport:
		hints = []keyHint{{"Enter", "Abrir função"}, {sym.updown, "Navegar"}, {"r", "Rota"}, {"e", "Exportar HTML"}, {"c", "Comparar"}, {"h", "Histórico"}, {"n", "Nova captura"}, {"Esc", "Voltar"}}
	case r.view == rtHistory:
		hints = []keyHint{{"Enter", "Abrir"}, {sym.updown, "Captura"}, {"D", "Remover"}, {"n", "Nova captura"}, {"Esc", "Voltar"}}
	}
	hints = append(hints, keyHint{"?", "Ajuda"})
	return renderHints(m.width, hints, 2)
}

// rtIndicator is the active-capture indicator of the top bar.
func (m *model) rtIndicator() string {
	r := m.rt
	if r.sess == nil {
		return ""
	}
	l := r.live
	return stDel.Render(sym.dot) + stSubtle.Render(" captura "+l.Elapsed.Round(time.Second).String()+" · "+strconv.Itoa(l.Requests)+" req")
}

var runtimeHelp = []keyHint{
	{"↑ ↓  Enter", "escolher e inspecionar um processo Go (pelo PID)"},
	{"p", "informar um PID"},
	{"1-7  d", "opções da captura e duração da fase ativa"},
	{"f  t", "intervalo de amostragem e porta da aplicação atrás do proxy"},
	{"r", "no relatório, filtra uma rota (alterna até voltar a todas)"},
	{"Enter", "iniciar / encerrar a captura; no relatório, abrir a função no Deep Inspector"},
	{"Espaço", "pausar / retomar a captura"},
	{"x", "cancelar a captura (nada é salvo)"},
	{"e", "exportar o relatório em HTML"},
	{"c", "comparar com outra captura (A antes, B depois)"},
	{"h", "histórico de capturas · D remove"},
	{"Esc", "voltar"},
}
