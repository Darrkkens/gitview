package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/athos/gitview/internal/audit"
	"github.com/athos/gitview/internal/audit/ai"
	"github.com/athos/gitview/internal/export"
	"github.com/athos/gitview/internal/flow"
	gmodel "github.com/athos/gitview/internal/graph/model"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// The CodeAudit screen runs the security scan in the background, lists the
// findings with their evidence, asks the local model about one when the
// user presses g, records review decisions and exports the Security
// Journey. Nothing blocks the git monitor; the model is optional.

const auditTimeout = 5 * time.Minute

type auditUI struct {
	running bool
	cancel  context.CancelFunc
	started time.Time
	a       *audit.Audit
	g       *gmodel.Graph
	prev    *audit.Audit
	store   *audit.Store
	reviews *audit.Reviews
	err     string

	sel     int
	view    string // list | detail | diff
	scroll  int
	results map[string]*ai.InvestigationResult

	aiRunning bool
	aiCancel  context.CancelFunc
	aiFor     string
	aiStatus  *ai.Status
	aiErr     string

	reviewing  bool
	pickStatus string
	note       textinput.Model

	writing  bool
	exported string
	msg      string
	msgErr   bool
}

type auditDoneMsg struct {
	a   *audit.Audit
	g   *gmodel.Graph
	err error
}

type auditAIMsg struct {
	id  string
	res ai.InvestigationResult
	err error
}

type auditProbeMsg struct{ st ai.Status }

type auditWrittenMsg struct {
	path string
	err  error
}

func newAuditUI() *auditUI {
	ti := textinput.New()
	ti.Prompt = "justificativa: "
	ti.TextStyle = stText
	ti.CharLimit = 2000
	return &auditUI{view: "list", results: map[string]*ai.InvestigationResult{}, note: ti}
}

func (m *model) auditStop() {
	if m.audit.cancel != nil {
		m.audit.cancel()
	}
	if m.audit.aiCancel != nil {
		m.audit.aiCancel()
	}
}

func (m *model) openAudit() tea.Cmd {
	m.screen = screenAudit
	au := m.audit
	if au.store == nil {
		st, err := audit.StoreFor(m.repo.Root, "")
		if err != nil {
			au.err = err.Error()
			return nil
		}
		au.store = st
		// The last audit is shown right away; r runs a new one.
		if cur, prev, err := st.Latest(); err == nil && cur != nil {
			au.a, au.prev = cur, prev
		}
	}
	au.reviews, _ = au.store.Reviews()
	return m.auditProbe()
}

func aiConfig() ai.Config { return ai.Config{}.FromEnv() }

func (m *model) auditProbe() tea.Cmd {
	return func() tea.Msg {
		cl, err := ai.NewClient(aiConfig())
		if err != nil {
			return auditProbeMsg{ai.Status{Error: err.Error()}}
		}
		return auditProbeMsg{cl.Probe(context.Background())}
	}
}

func (m *model) auditStart(exportAfter bool) tea.Cmd {
	au := m.audit
	if au.running {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), auditTimeout)
	au.running, au.cancel, au.started, au.err, au.msg = true, cancel, time.Now(), "", ""
	root, cache := m.repo.Root, m.flowCache
	return func() tea.Msg {
		defer cancel()
		start := time.Now()
		var progs []*flow.Program
		var failures []string
		for _, d := range flow.FindModules(root) {
			p, _, err := cache.Get(ctx, root, d)
			if ctx.Err() != nil {
				return auditDoneMsg{err: ctx.Err()}
			}
			if err != nil {
				rel, _ := filepath.Rel(root, d)
				failures = append(failures, rel+": "+err.Error())
				continue
			}
			progs = append(progs, p)
		}
		g, err := buildRepoGraph(ctx, root, progs, failures, time.Since(start), false)
		if err != nil {
			return auditDoneMsg{err: err}
		}
		a, err := auditGraph(ctx, root, progs, g, auditFlags{vulndb: envVulnDB()})
		return auditDoneMsg{a: a, g: g, err: err}
	}
}

func (m *model) auditDone(msg auditDoneMsg) {
	au := m.audit
	au.running, au.cancel = false, nil
	if msg.err != nil {
		au.err = msg.err.Error()
		if errors.Is(msg.err, context.Canceled) {
			au.err = "análise cancelada"
		}
		return
	}
	au.prev = au.a
	au.a, au.g = msg.a, msg.g
	if _, err := au.store.Save(msg.a); err != nil {
		au.msg, au.msgErr = "snapshot não gravado: "+err.Error(), true
	}
	au.sel, au.scroll, au.view = 0, 0, "list"
}

func (m *model) auditSelected() *audit.Finding {
	au := m.audit
	if au.a == nil || au.sel < 0 || au.sel >= len(au.a.Findings) {
		return nil
	}
	return au.a.Findings[au.sel]
}

func (m *model) auditExplain() tea.Cmd {
	au := m.audit
	f := m.auditSelected()
	if f == nil || au.aiRunning {
		return nil
	}
	cl, err := ai.NewClient(aiConfig())
	if err != nil {
		au.msg, au.msgErr = err.Error(), true
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	au.aiRunning, au.aiCancel, au.aiFor, au.aiErr = true, cancel, f.ID, ""
	iv := &ai.Investigator{Client: cl, Cache: au.store, Read: repoReader(m.repo.Root)}
	return func() tea.Msg {
		defer cancel()
		res, err := iv.ExplainFinding(ctx, ai.InvestigationRequest{Finding: f})
		return auditAIMsg{id: f.ID, res: res, err: err}
	}
}

func (m *model) auditAIDone(msg auditAIMsg) {
	au := m.audit
	au.aiRunning, au.aiCancel = false, nil
	if msg.err != nil {
		au.aiErr = msg.err.Error()
		if errors.Is(msg.err, context.Canceled) {
			au.aiErr = "pedido cancelado"
		}
		return
	}
	r := msg.res
	au.results[msg.id] = &r
	au.view, au.scroll = "detail", 0
}

func (m *model) auditExport() tea.Cmd {
	au := m.audit
	if au.writing {
		return nil
	}
	if au.g == nil {
		au.msg, au.msgErr = "rode a análise (r) antes de exportar: o relatório usa o grafo atual", true
		return nil
	}
	au.writing, au.msg = true, ""
	g, a, store, root := au.g, au.a, au.store, m.repo.Root
	path := filepath.Join(m.export.dir, "gitview-security-"+time.Now().Format("20060102-150405")+".html")
	return func() tea.Msg {
		data, err := securityPage(g, a, store, root)
		if err == nil {
			err = export.WriteFile(path, data, false)
		}
		return auditWrittenMsg{path: path, err: err}
	}
}

func (m *model) auditKey(msg tea.KeyMsg) tea.Cmd {
	au := m.audit
	k := msg.String()
	if au.reviewing {
		switch k {
		case "esc":
			au.reviewing = false
			au.note.Blur()
		case "enter":
			m.auditDecide(au.pickStatus, au.note.Value())
		default:
			var cmd tea.Cmd
			au.note, cmd = au.note.Update(msg)
			return cmd
		}
		return nil
	}
	switch k {
	case "esc", "q":
		switch {
		case au.running:
			au.cancel()
		case au.aiRunning:
			au.aiCancel()
		case au.view != "list":
			au.view, au.scroll = "list", 0
		default:
			m.screen = screenDiff
		}
		return nil
	case "?":
		m.showHelp = true
	case "r":
		return m.auditStart(false)
	case "g":
		return m.auditExplain()
	case "m":
		return m.auditProbe()
	case "x":
		return m.auditExport()
	case "o":
		if au.exported != "" {
			au.msg, au.msgErr = "abrindo "+au.exported+" no navegador…", false
			return openInBrowser(au.exported)
		}
		au.msg, au.msgErr = "exporte o relatório (x) antes de abrir", true
	case "c":
		if au.a == nil || au.prev == nil {
			au.msg, au.msgErr = "são precisas duas auditorias para comparar", true
			return nil
		}
		au.view, au.scroll = "diff", 0
	case "enter":
		if au.view == "list" && m.auditSelected() != nil {
			au.view, au.scroll = "detail", 0
		}
	case "up", "k":
		if au.view == "list" {
			au.sel = max(0, au.sel-1)
		} else {
			au.scroll = max(0, au.scroll-1)
		}
	case "down", "j":
		if au.view == "list" {
			if au.a != nil && au.sel < len(au.a.Findings)-1 {
				au.sel++
			}
		} else {
			au.scroll++
		}
	case "pgdown", "ctrl+d":
		au.scroll += m.vp.Height / 2
	case "pgup", "ctrl+u":
		au.scroll = max(0, au.scroll-m.vp.Height/2)
	case "1", "2", "3", "4", "5", "6", "7":
		if au.view != "detail" {
			return nil
		}
		st := audit.Statuses[int(k[0]-'1')]
		au.pickStatus = st
		au.note.SetValue("")
		au.reviewing = true
		au.note.Focus()
		return textinput.Blink
	}
	return nil
}

func (m *model) auditDecide(status, note string) {
	au := m.audit
	f := m.auditSelected()
	if f == nil {
		return
	}
	rec, err := au.reviews.Decide(f, status, note, au.a.Commit, time.Now())
	if err != nil {
		au.msg, au.msgErr = err.Error(), true
		return
	}
	if err := au.store.SaveReviews(au.reviews); err != nil {
		au.msg, au.msgErr = err.Error(), true
		return
	}
	au.reviewing = false
	au.note.Blur()
	last := rec.History[len(rec.History)-1]
	au.msg, au.msgErr = fmt.Sprintf("decisão registrada: %s → %s", last.From, last.To), false
}

var auditStatusKeys = "1 open · 2 investigating · 3 confirmed · 4 false_positive · 5 accepted_risk · 6 fixed · 7 needs_revalidation"

func sevStyle(sev string) string {
	switch sev {
	case audit.SevCritical, audit.SevHigh:
		return stDel.Render(fmt.Sprintf("%-8s", sev))
	case audit.SevMedium:
		return stAccent.Render(fmt.Sprintf("%-8s", sev))
	}
	return stMuted.Render(fmt.Sprintf("%-8s", sev))
}

func (m *model) auditView() string {
	au := m.audit
	var b strings.Builder
	b.WriteString(" " + stBold.Render("CodeAudit") + stMuted.Render("  auditoria de segurança estática, com evidências; a IA (Ollama local) é opcional") + "\n")
	switch {
	case au.aiStatus == nil:
		b.WriteString(" " + stMuted.Render("Ollama: verificando…") + "\n")
	case au.aiStatus.Reachable && au.aiStatus.Selected != "":
		b.WriteString(" " + stAdd.Render("Ollama: "+au.aiStatus.Selected) + stMuted.Render(" em "+au.aiStatus.Endpoint+" · g explica o finding selecionado") + "\n")
	default:
		b.WriteString(" " + stMuted.Render("Ollama indisponível ("+au.aiStatus.Error+"); a análise funciona sem ele") + "\n")
	}
	b.WriteString("\n")
	if au.running {
		b.WriteString(" " + stAccent.Render("analisando…") + stMuted.Render(fmt.Sprintf("  %s · Esc cancela · nada do repositório é executado", time.Since(au.started).Round(time.Second))) + "\n")
		return b.String()
	}
	if au.err != "" {
		b.WriteString(" " + stDel.Render(au.err) + "\n")
	}
	if au.a == nil {
		b.WriteString(" " + stMuted.Render("Nenhuma auditoria ainda. r analisa o repositório.") + "\n")
		return b.String() + m.auditMsg()
	}
	a := au.a
	commit := a.Commit
	if len(commit) > 12 {
		commit = commit[:12]
	}
	bySev := map[string]int{}
	for _, f := range a.Findings {
		bySev[f.Severity]++
	}
	b.WriteString(fmt.Sprintf(" %s %s · commit %s · análise %s · %s\n", stSubtle.Render("Auditoria"), a.Repository, commit, a.Status.Status, a.Generated.Local().Format("02/01 15:04")))
	b.WriteString(fmt.Sprintf("   %d findings: %d critical, %d high, %d medium, %d low, %d info\n\n", len(a.Findings), bySev["critical"], bySev["high"], bySev["medium"], bySev["low"], bySev["info"]))
	switch au.view {
	case "detail":
		b.WriteString(m.auditDetail())
	case "diff":
		var buf bytes.Buffer
		d := audit.Compare(au.prev, au.a)
		fmt.Fprintf(&buf, "Comparação com a auditoria anterior (commit %s)\n\nNew %d · Resolved %d · Unchanged %d · Changed %d · Not confirmed gone %d\n", shortHash(d.BaseCommit), len(d.New), len(d.Resolved), len(d.Unchanged), len(d.Changed), len(d.Unconfirmed))
		for _, x := range [][2]any{{"New", d.New}, {"Resolved", d.Resolved}, {"Changed", d.Changed}, {"Not confirmed gone", d.Unconfirmed}} {
			items := x[1].([]audit.DiffItem)
			if len(items) == 0 {
				continue
			}
			fmt.Fprintf(&buf, "\n%s\n", x[0])
			for _, it := range items {
				fmt.Fprintf(&buf, "  %-8s %-13s %s:%d %s %s\n", it.Severity, it.RuleID, it.File, it.Line, it.Title, strings.Join(append(it.Changes, it.Reason), " "))
			}
		}
		b.WriteString(m.auditScrolled(buf.String()))
	default:
		if len(a.Findings) == 0 {
			b.WriteString("   " + stMuted.Render("Nenhum finding: as regras habilitadas não encontraram nada. Veja os limites da análise no relatório (x).") + "\n")
		}
		rows := max(3, m.vp.Height-8)
		start := max(0, au.sel-rows/2)
		for i := start; i < len(a.Findings) && i < start+rows; i++ {
			f := a.Findings[i]
			mark := "  "
			st := stText
			if i == au.sel {
				mark, st = stAccent.Render(sym.more)+" ", stAccent
			}
			status := au.reviews.Status(f.ID)
			b.WriteString(fmt.Sprintf(" %s%s %-10s %-13s %s  %s\n", mark, sevStyle(f.Severity), f.Verdict, status, st.Render(f.Title), stMuted.Render(fmt.Sprintf("%s:%d", f.File, f.Line))))
		}
	}
	return b.String() + m.auditMsg()
}

func shortHash(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

func (m *model) auditMsg() string {
	au := m.audit
	var b strings.Builder
	if au.aiRunning {
		b.WriteString("\n " + stAccent.Render("perguntando ao modelo local…") + stMuted.Render("  Esc cancela") + "\n")
	}
	if au.aiErr != "" {
		b.WriteString("\n " + stDel.Render("IA: "+au.aiErr) + "\n")
	}
	if au.writing {
		b.WriteString("\n " + stAccent.Render("gravando o relatório…") + "\n")
	}
	if au.reviewing {
		b.WriteString("\n " + stSubtle.Render("novo estado "+au.pickStatus+" · Enter grava · Esc cancela") + "\n " + au.note.View() + "\n")
	}
	if au.msg != "" {
		st := stAdd
		if au.msgErr {
			st = stDel
		}
		b.WriteString("\n " + st.Render(au.msg) + "\n")
	}
	return b.String()
}

func (m *model) auditDetail() string { return m.auditScrolled(m.auditDetailText()) }

// auditDetailText is the whole detail of the selected finding.
func (m *model) auditDetailText() string {
	au := m.audit
	f := m.auditSelected()
	if f == nil {
		return ""
	}
	var buf bytes.Buffer
	printFinding(&buf, m.repo.Root, au.a, f, au.reviews)
	if rec := au.reviews.Findings[f.ID]; rec != nil && len(rec.History) > 0 {
		buf.WriteString("\n")
		printHistory(&buf, f, rec)
	}
	if res := au.results[f.ID]; res != nil {
		buf.WriteString("\n")
		printInvestigation(&buf, *res)
	}
	buf.WriteString("\nrevisão: " + auditStatusKeys + "\n")
	return buf.String()
}

func (m *model) auditScrolled(text string) string {
	au := m.audit
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	rows := max(3, m.vp.Height-6)
	if au.scroll > max(0, len(lines)-rows) {
		au.scroll = max(0, len(lines)-rows)
	}
	end := min(len(lines), au.scroll+rows)
	var b strings.Builder
	for _, l := range lines[au.scroll:end] {
		b.WriteString("  " + l + "\n")
	}
	if end < len(lines) {
		b.WriteString(stMuted.Render(fmt.Sprintf("  … %d linhas · ↓ PgDn", len(lines)-end)) + "\n")
	}
	return b.String()
}

func (m *model) auditFooter() string {
	au := m.audit
	if au.running || au.aiRunning {
		return renderHints(m.width, []keyHint{{"Esc", "Cancelar"}}, 1)
	}
	if au.reviewing {
		return renderHints(m.width, []keyHint{{"Enter", "Gravar"}, {"Esc", "Cancelar"}}, 2)
	}
	hints := []keyHint{{"r", "Analisar"}}
	if au.a != nil {
		hints = append(hints, keyHint{sym.updown, "Findings"}, keyHint{"Enter", "Detalhes"}, keyHint{"g", "Explain with Gemma"}, keyHint{"c", "Comparar"}, keyHint{"x", "Relatório"})
	}
	if au.view == "detail" {
		hints = append(hints, keyHint{"1-7", "Revisar"})
	}
	if au.exported != "" {
		hints = append(hints, keyHint{"o", "Abrir"})
	}
	hints = append(hints, keyHint{"?", "Ajuda"}, keyHint{"Esc", "Voltar"})
	return renderHints(m.width, hints, 2)
}

var auditHelp = []keyHint{
	{"r", "analisar o repositório (em segundo plano; Esc cancela)"},
	{"↑ ↓  Enter", "escolher um finding e ver evidências, código, caminho e rotas"},
	{"g", "Explain with Gemma: pergunta ao Ollama local sobre o finding (Esc cancela)"},
	{"1 … 7", "revisar: open, investigating, confirmed, false_positive, accepted_risk, fixed, needs_revalidation"},
	{"c", "comparar com a auditoria anterior"},
	{"x  o", "exportar o Security Journey (HTML offline) e abri-lo"},
	{"m", "verificar o Ollama de novo (GITVIEW_OLLAMA_URL, GITVIEW_AI_MODEL)"},
	{"Esc", "voltar / cancelar"},
	{"", "gitview audit serve abre o Security Journey com IA e revisão no navegador"},
}
