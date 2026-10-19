package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/athos/gitview/internal/export"
	"github.com/athos/gitview/internal/flow"
	gexport "github.com/athos/gitview/internal/graph/export"
	gmodel "github.com/athos/gitview/internal/graph/model"
	tea "github.com/charmbracelet/bubbletea"
)

// The Repository Graph screen controls the analysis and the export; the graph
// itself is explored in the HTML page, which is opened in the browser only
// when the user asks for it.

const graphLoadTimeout = 5 * time.Minute

type graphScope struct {
	label string
	dirs  []string // module directories
}

type graphUI struct {
	scopes  []graphScope
	sel     int
	running bool
	cancel  context.CancelFunc
	started time.Time
	scope   string // label of the analysed scope
	g       *gmodel.Graph
	err     string

	writing  bool
	exported string // last written file
	msg      string
	msgErr   bool
}

type graphDoneMsg struct {
	g   *gmodel.Graph
	err error
}

type graphWrittenMsg struct {
	path string
	err  error
}

type graphOpenedMsg struct{ err error }

func newGraphUI() *graphUI { return &graphUI{} }

func (m *model) graphStop() {
	if m.graph.cancel != nil {
		m.graph.cancel()
	}
}

func (m *model) openGraph() tea.Cmd {
	m.screen = screenGraph
	gu := m.graph
	if len(gu.scopes) == 0 {
		root := m.repo.Root
		mods := flow.FindModules(root)
		if len(mods) > 1 {
			gu.scopes = append(gu.scopes, graphScope{label: fmt.Sprintf("Repositório inteiro (%d módulos)", len(mods)), dirs: mods})
		}
		for _, d := range mods {
			rel, _ := filepath.Rel(root, d)
			gu.scopes = append(gu.scopes, graphScope{label: "Módulo " + rel, dirs: []string{d}})
		}
	}
	return nil
}

func (m *model) graphStart() tea.Cmd {
	gu := m.graph
	if gu.running || len(gu.scopes) == 0 {
		return nil
	}
	sc := gu.scopes[gu.sel]
	ctx, cancel := context.WithTimeout(context.Background(), graphLoadTimeout)
	gu.running, gu.cancel, gu.started, gu.err, gu.msg = true, cancel, time.Now(), "", ""
	gu.scope = sc.label
	root, cache := m.repo.Root, m.flowCache
	return func() tea.Msg {
		defer cancel()
		start := time.Now()
		var progs []*flow.Program
		var failures []string
		for _, d := range sc.dirs {
			p, _, err := cache.Get(ctx, root, d)
			if ctx.Err() != nil {
				return graphDoneMsg{err: ctx.Err()}
			}
			if err != nil {
				rel, _ := filepath.Rel(root, d)
				failures = append(failures, rel+": "+err.Error())
				continue
			}
			progs = append(progs, p)
		}
		g, err := buildRepoGraph(ctx, root, progs, failures, time.Since(start), false)
		return graphDoneMsg{g: g, err: err}
	}
}

func (m *model) graphDone(msg graphDoneMsg) {
	gu := m.graph
	gu.running, gu.cancel = false, nil
	if msg.err != nil {
		gu.err = msg.err.Error()
		if errors.Is(msg.err, context.Canceled) {
			gu.err = "análise cancelada"
		}
		return
	}
	gu.g, gu.exported = msg.g, ""
}

// graphDest names a new file in the export directory; a timestamp keeps
// earlier exports from being overwritten.
func (m *model) graphDest(ext string) string {
	return filepath.Join(m.export.dir, "gitview-graph-"+time.Now().Format("20060102-150405")+ext)
}

func (m *model) graphWrite(format string) tea.Cmd {
	gu := m.graph
	if gu.g == nil || gu.writing {
		return nil
	}
	gu.writing, gu.msg = true, ""
	g := gu.g
	path := m.graphDest("." + format)
	return func() tea.Msg {
		var buf bytes.Buffer
		var err error
		if format == "json" {
			err = gexport.JSON(&buf, g, "")
		} else {
			// The positions saved by gitview serve, when there are some.
			layout, _ := savedLayout(g.Root)
			err = gexport.HTML(&buf, g, gexport.HTMLOptions{Layout: layout})
		}
		if err == nil {
			err = export.WriteFile(path, buf.Bytes(), false)
		}
		return graphWrittenMsg{path: path, err: err}
	}
}

func (m *model) graphWritten(msg graphWrittenMsg) {
	gu := m.graph
	gu.writing = false
	if msg.err != nil {
		gu.msg, gu.msgErr = "falha ao gravar: "+msg.err.Error(), true
		return
	}
	gu.exported, gu.msg, gu.msgErr = msg.path, "gravado em "+msg.path, false
}

// startBrowser runs the platform's opener on a local file (replaced in
// tests).
var startBrowser = func(abs string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", abs)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", abs)
	default:
		cmd = exec.Command("xdg-open", abs)
	}
	return cmd.Start()
}

// openInBrowser opens a local file; it is only called from an explicit key
// press.
func openInBrowser(path string) tea.Cmd {
	return func() tea.Msg {
		abs, err := filepath.Abs(path)
		if err != nil {
			return graphOpenedMsg{err}
		}
		return graphOpenedMsg{startBrowser(abs)}
	}
}

func (m *model) graphKey(msg tea.KeyMsg) tea.Cmd {
	gu := m.graph
	switch msg.String() {
	case "esc", "q":
		if gu.running {
			gu.cancel()
			return nil
		}
		m.screen = screenDiff
	case "?":
		m.showHelp = true
	case "up", "k":
		if !gu.running && gu.sel > 0 {
			gu.sel--
		}
	case "down", "j":
		if !gu.running && gu.sel < len(gu.scopes)-1 {
			gu.sel++
		}
	case "enter", "r":
		return m.graphStart()
	case "x":
		return m.graphWrite("html")
	case "J":
		return m.graphWrite("json")
	case "o":
		if gu.exported != "" && strings.HasSuffix(gu.exported, ".html") {
			gu.msg, gu.msgErr = "abrindo "+gu.exported+" no navegador…", false
			return openInBrowser(gu.exported)
		}
		gu.msg, gu.msgErr = "exporte o HTML (x) antes de abrir", true
	}
	return nil
}

func (m *model) graphView() string {
	gu := m.graph
	var b strings.Builder
	b.WriteString(" " + stBold.Render("Repository Graph") + stMuted.Render("  pacotes, arquivos, tipos e funções como um grafo navegável (análise estática)") + "\n\n")
	if len(gu.scopes) == 0 {
		b.WriteString("   " + stMuted.Render("Nenhum módulo Go (go.mod) neste repositório.") + "\n")
		return b.String()
	}
	b.WriteString(" " + stSubtle.Render("Escopo") + "\n")
	for i, sc := range gu.scopes {
		mark, st := "  ", stText
		if i == gu.sel {
			mark, st = stAccent.Render(sym.more)+" ", stAccent
		}
		b.WriteString("  " + mark + st.Render(sc.label) + "\n")
	}
	b.WriteString("\n")
	switch {
	case gu.running:
		b.WriteString(" " + stAccent.Render("analisando "+gu.scope+"…") + stMuted.Render(fmt.Sprintf("  %s · Esc cancela · o Git continua sendo monitorado", time.Since(gu.started).Round(time.Second))) + "\n")
	case gu.err != "":
		b.WriteString(" " + stDel.Render(gu.err) + "\n")
	case gu.g == nil:
		b.WriteString(" " + stMuted.Render("Enter analisa o escopo selecionado. Nada do repositório é executado.") + "\n")
	}
	if g := gu.g; g != nil && !gu.running {
		b.WriteString(" " + stSubtle.Render("Estatísticas") + stMuted.Render(" · "+gu.scope) + "\n")
		for _, l := range graphStatsLines(g) {
			b.WriteString("   " + stMuted.Render(fmt.Sprintf("%-22s", l[0])) + stText.Render(l[1]) + "\n")
		}
		if g.Completeness.Status != "complete" {
			reasons := g.Completeness.Reasons
			b.WriteString("   " + stMuted.Render(fmt.Sprintf("%-22s", "Completeness")) + stSubtle.Render(string(g.Completeness.Status)+": "+strings.Join(reasons[:min(len(reasons), 2)], "; ")) + "\n")
		}
		b.WriteString("\n " + stSubtle.Render("Exportação") + stMuted.Render("  "+filepath.Join(m.export.dir, "gitview-graph-<data>.html|json")) + "\n")
		if gu.writing {
			b.WriteString("   " + stAccent.Render("gravando…") + "\n")
		}
	}
	if gu.msg != "" {
		st := stAdd
		if gu.msgErr {
			st = stDel
		}
		b.WriteString("   " + st.Render(gu.msg) + "\n")
	}
	return b.String()
}

func (m *model) graphFooter() string {
	gu := m.graph
	if gu.running {
		return renderHints(m.width, []keyHint{{"Esc", "Cancelar"}}, 1)
	}
	hints := []keyHint{{sym.updown, "Escopo"}, {"Enter", "Analisar"}}
	if gu.g != nil {
		hints = append(hints, keyHint{"x", "Exportar HTML"}, keyHint{"J", "JSON"})
	}
	if gu.exported != "" && strings.HasSuffix(gu.exported, ".html") {
		hints = append(hints, keyHint{"o", "Abrir no navegador"})
	}
	hints = append(hints, keyHint{"?", "Ajuda"}, keyHint{"Esc", "Voltar"})
	return renderHints(m.width, hints, 2)
}

var graphHelp = []keyHint{
	{"↑ ↓", "escolher o escopo: repositório inteiro ou um módulo"},
	{"Enter  r", "analisar (em segundo plano; reaproveita a análise do Code Flow)"},
	{"x", "exportar o HTML interativo (arquivo novo, nunca sobrescreve)"},
	{"J", "exportar o dataset JSON (schema gitview.graph/v1)"},
	{"o", "abrir o último HTML exportado no navegador"},
	{"Esc", "cancelar a análise / voltar"},
}
