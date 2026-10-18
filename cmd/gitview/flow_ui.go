package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

const flowLoadTimeout = 2 * time.Minute

type flowPhase int

const (
	phasePick flowPhase = iota
	phaseGraph
)

type flowCand struct {
	prog *flow.Program
	sym  flow.Symbol
}

type flowRow struct {
	id     string
	indent int
	edge   *flow.Edge // nil for the root and section headers
	up     bool
	header string
	cycle  bool
}

type flowUI struct {
	input   textinput.Model
	phase   flowPhase
	loading bool
	cancel  context.CancelFunc
	err     string
	progs   []*flow.Program
	cands   []flowCand
	csel    int
	initial string // file selected in the diff when the screen opened

	prog    *flow.Program
	graph   *flow.Graph
	rootID  string
	dir     string
	depth   int
	rows    []flowRow
	rsel    int
	history []string
	msg     string
}

type flowLoadedMsg struct {
	progs []*flow.Program
	err   error
}

func newFlowUI() *flowUI {
	ti := textinput.New()
	ti.Prompt = "  Find Function: "
	ti.Placeholder = "nome, pacote, receiver, arquivo ou nome qualificado"
	ti.PromptStyle = stAccent
	ti.TextStyle = stText
	ti.PlaceholderStyle = stMuted
	return &flowUI{input: ti, dir: flow.Both, depth: 5}
}

// openFlow starts (or reuses) the background analysis of the repository's
// Go modules. Git monitoring keeps running meanwhile.
func (m *model) openFlow() tea.Cmd {
	f := m.flow
	m.screen = screenFlow
	f.phase, f.msg, f.err = phasePick, "", ""
	f.initial = m.cursor
	f.input.SetValue("")
	f.input.Width = max(m.width-22, 20)
	f.input.Focus()
	if f.loading {
		return textinput.Blink
	}
	f.loading = true
	ctx, cancel := context.WithTimeout(context.Background(), flowLoadTimeout)
	f.cancel = cancel
	root, cache := m.repo.Root, m.flowCache
	return tea.Batch(textinput.Blink, func() tea.Msg {
		defer cancel()
		mods := flow.FindModules(root)
		if len(mods) == 0 {
			return flowLoadedMsg{err: fmt.Errorf("nenhum módulo Go (go.mod) no repositório")}
		}
		var progs []*flow.Program
		var firstErr error
		for _, mod := range mods {
			p, _, err := cache.Get(ctx, root, mod)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			progs = append(progs, p)
		}
		if len(progs) == 0 {
			return flowLoadedMsg{err: firstErr}
		}
		return flowLoadedMsg{progs: progs}
	})
}

func (m *model) flowLoaded(msg flowLoadedMsg) {
	f := m.flow
	f.loading = false
	if msg.err != nil {
		f.err = msg.err.Error()
		return
	}
	f.progs = msg.progs
	m.flowCandidates()
}

// flowCandidates lists symbols for the query; with an empty query it shows the
// functions modified in the file selected in the diff, then in the whole tree.
func (m *model) flowCandidates() {
	f := m.flow
	q := strings.TrimSpace(f.input.Value())
	f.cands = f.cands[:0]
	if q == "" {
		changed := m.changedLinesOf()
		for _, p := range f.progs {
			for _, c := range p.ChangedFunctions(changed) {
				for _, s := range p.Find(c.ID) {
					f.cands = append(f.cands, flowCand{p, s})
				}
			}
		}
		sort.SliceStable(f.cands, func(i, j int) bool {
			return f.cands[i].sym.File == f.initial && f.cands[j].sym.File != f.initial
		})
	} else {
		lq := strings.ToLower(q)
		type scored struct {
			c     flowCand
			score int
		}
		var res []scored
		for _, p := range f.progs {
			exact := map[string]bool{}
			for _, s := range p.Find(q) {
				exact[s.ID] = true
			}
			for _, s := range p.Symbols() {
				hay := strings.ToLower(s.Qualified + " " + s.Pkg + " " + s.File)
				sc := -1
				switch {
				case exact[s.ID]:
					sc = 0
				case strings.HasPrefix(strings.ToLower(s.Name), lq):
					sc = 1
				case containsAll(hay, strings.Fields(lq)):
					sc = 2
				}
				if sc >= 0 {
					res = append(res, scored{flowCand{p, s}, sc})
				}
			}
		}
		sort.SliceStable(res, func(i, j int) bool {
			if res[i].score != res[j].score {
				return res[i].score < res[j].score
			}
			return res[i].c.sym.Qualified < res[j].c.sym.Qualified
		})
		for i, r := range res {
			if i >= 200 {
				break
			}
			f.cands = append(f.cands, r.c)
		}
	}
	f.csel = min(f.csel, max(len(f.cands)-1, 0))
}

func containsAll(hay string, toks []string) bool {
	for _, t := range toks {
		if !strings.Contains(hay, t) {
			return false
		}
	}
	return len(toks) > 0
}

// flowQuery builds the graph for the chosen symbol.
func (m *model) flowQuery(p *flow.Program, id string, pushHistory bool) {
	f := m.flow
	fn, err := p.Resolve(id)
	if err != nil {
		f.msg = err.Error()
		return
	}
	g, err := p.Query(fn, f.dir, flow.Options{Depth: f.depth, External: true, Changed: m.changedLinesOf()})
	if err != nil {
		f.msg = err.Error()
		return
	}
	if pushHistory && f.rootID != "" && f.rootID != id {
		f.history = append(f.history, f.rootID)
	}
	f.prog, f.graph, f.rootID, f.phase, f.msg = p, g, id, phaseGraph, ""
	f.input.Blur()
	f.rows = flowRows(g, id, f.dir)
	f.rsel = 0
	for i, r := range f.rows {
		if r.id == id && r.edge == nil && r.header == "" {
			f.rsel = i
		}
	}
}

// flowRows flattens the graph into an indented tree: callers above the
// root (each level further up), callees below.
func flowRows(g *flow.Graph, root, dir string) []flowRow {
	var rows []flowRow
	var walk func(id string, up bool, indent int, path map[string]bool)
	walk = func(id string, up bool, indent int, path map[string]bool) {
		edges := g.Out(id)
		if up {
			edges = g.In(id)
		}
		sort.SliceStable(edges, func(i, j int) bool { return edges[i].Order < edges[j].Order })
		for _, e := range edges {
			other := e.To
			if up {
				other = e.From
			}
			if g.Node(other) == nil {
				continue
			}
			r := flowRow{id: other, indent: indent, edge: e, up: up, cycle: path[other]}
			rows = append(rows, r)
			if r.cycle {
				continue
			}
			path[other] = true
			walk(other, up, indent+1, path)
			delete(path, other)
		}
	}
	if dir != flow.Callees {
		rows = append(rows, flowRow{header: "Callers"})
		n := len(rows)
		walk(root, true, 1, map[string]bool{root: true})
		if len(rows) == n {
			rows = append(rows, flowRow{header: "  (nenhum chamador encontrado)"})
		}
	}
	rows = append(rows, flowRow{id: root})
	if dir != flow.Callers {
		rows = append(rows, flowRow{header: "Callees"})
		n := len(rows)
		walk(root, false, 1, map[string]bool{root: true})
		if len(rows) == n {
			rows = append(rows, flowRow{header: "  (nenhuma chamada)"})
		}
	}
	return rows
}

func (m *model) flowKey(msg tea.KeyMsg) tea.Cmd {
	f := m.flow
	key := msg.String()
	if f.phase == phasePick {
		switch key {
		case "esc":
			if f.loading && f.cancel != nil {
				f.cancel()
			}
			if f.graph != nil && f.input.Value() != "" {
				f.phase = phaseGraph
				f.input.Blur()
				return nil
			}
			m.screen = screenDiff
			return nil
		case "up", "ctrl+p", "shift+tab":
			f.csel = max(f.csel-1, 0)
			return nil
		case "down", "ctrl+n", "tab":
			f.csel = min(f.csel+1, max(len(f.cands)-1, 0))
			return nil
		case "enter":
			if f.csel < len(f.cands) {
				c := f.cands[f.csel]
				f.history = nil
				m.flowQuery(c.prog, c.sym.ID, false)
				m.insp.sel, m.insp.off = 0, 0
				return m.inspRefresh(false)
			}
			return nil
		}
		before := f.input.Value()
		var cmd tea.Cmd
		f.input, cmd = f.input.Update(msg)
		if f.input.Value() != before && !f.loading {
			m.flowCandidates()
		}
		return cmd
	}

	if m.insp.tab != tabCodeFlow && m.insp.modal() {
		return m.inspKey(msg)
	}
	if cmd, ok := m.inspTabKey(key); ok {
		return cmd
	}
	if m.insp.tab != tabCodeFlow {
		return m.inspKey(msg)
	}
	switch key {
	case "esc", "q":
		m.leaveInspector()
	case "/":
		f.phase = phasePick
		f.input.Width = max(m.width-22, 20)
		f.input.Focus()
		m.flowCandidates()
		return textinput.Blink
	case "?":
		m.showHelp = true
	case "up", "k":
		f.rsel = m.flowStep(-1)
	case "down", "j":
		f.rsel = m.flowStep(1)
	case "tab":
		f.dir = map[string]string{flow.Both: flow.Callers, flow.Callers: flow.Callees, flow.Callees: flow.Both}[f.dir]
		m.flowQuery(f.prog, f.rootID, false)
	case "+", "=":
		f.depth = min(f.depth+1, 5)
		m.flowQuery(f.prog, f.rootID, false)
	case "-":
		f.depth = max(f.depth-1, 1)
		m.flowQuery(f.prog, f.rootID, false)
	case "enter":
		if r := f.selectedRow(); r != nil {
			n := f.graph.Node(r.id)
			if n != nil && (n.Kind == flow.KindFunc || n.Kind == flow.KindClosure) && r.id != f.rootID {
				m.flowQuery(f.prog, r.id, true)
			}
		}
	case "backspace":
		if l := len(f.history); l > 0 {
			prev := f.history[l-1]
			f.history = f.history[:l-1]
			m.flowQuery(f.prog, prev, false)
		}
	case "g", "o":
		if r := f.selectedRow(); r != nil {
			if n := f.graph.Node(r.id); n != nil && n.File != "" {
				if !m.gotoLine(n.File, n.Line, n.EndLine) {
					f.msg = fmt.Sprintf("%s:%d não foi modificado no working tree", n.File, n.Line)
				}
			}
		}
	}
	return nil
}

func (f *flowUI) selectedRow() *flowRow {
	if f.rsel >= 0 && f.rsel < len(f.rows) && f.rows[f.rsel].header == "" {
		return &f.rows[f.rsel]
	}
	return nil
}

func (m *model) flowStep(dir int) int {
	f := m.flow
	i := f.rsel
	for {
		i += dir
		if i < 0 || i >= len(f.rows) {
			return f.rsel
		}
		if f.rows[i].header == "" {
			return i
		}
	}
}

// gotoLine opens the diff of file at the first visible line of the range.
func (m *model) gotoLine(file string, from, to int) bool {
	pd := m.parsed[file]
	if pd == nil {
		return false
	}
	target := -1
	for i, l := range pd.lines {
		if l.kind != kindDel && l.kind != kindHunk && l.num >= from && l.num <= max(to, from) {
			target = i
			break
		}
	}
	if target < 0 {
		return false
	}
	m.screen = screenDiff
	m.cursor = file
	m.expanded[file] = true
	m.rebuild()
	vi := m.cursorIndex()
	if vi >= len(m.headers) {
		return true
	}
	row := m.headers[vi]
	if e := m.renderCache[file]; target < len(e.rows) && e.rows[target] >= 0 {
		row += 1 + e.rows[target]
	}
	m.vp.SetYOffset(max(row-m.vp.Height/3, 0))
	return true
}

// --- rendering -----------------------------------------------------------------

func (m *model) flowView() string {
	f := m.flow
	w, h := m.width, m.vp.Height
	if f.phase == phasePick {
		return m.flowPickView(w, h)
	}
	if m.insp.tab != tabCodeFlow {
		return m.inspView(w, h)
	}
	n := f.graph.Node(f.rootID)
	right := stMuted.Render(fmt.Sprintf("%s · profundidade %d", f.dir, f.depth))
	title := " " + stBold.Render("Code Flow") + "  " + stSubtle.Render(n.Name)
	lines := []string{m.inspTabBar(w), title + strings.Repeat(" ", max(w-lipgloss.Width(title)-lipgloss.Width(right)-1, 1)) + right, ""}

	detail := m.flowDetail(w)
	listH := max(h-len(lines)-len(detail)-1, 3)
	start := max(f.rsel-listH/2, 0)
	start = min(start, max(len(f.rows)-listH, 0))
	for i := start; i < len(f.rows) && i-start < listH; i++ {
		lines = append(lines, m.flowRowView(f.rows[i], i == f.rsel, w))
	}
	for len(lines) < h-len(detail) {
		lines = append(lines, "")
	}
	return strings.Join(append(lines, detail...), "\n")
}

func (m *model) flowPickView(w, h int) string {
	f := m.flow
	lines := []string{f.input.View(), ""}
	switch {
	case f.loading:
		lines = append(lines, "  "+stMuted.Render("Analisando o código Go em segundo plano… (Esc cancela)"))
	case f.err != "":
		lines = append(lines, "  "+stDel.Render(ansiTruncate("Code Flow indisponível: "+f.err, w-3)))
	default:
		label := fmt.Sprintf("%d símbolos", len(f.cands))
		if f.input.Value() == "" {
			label = "Funções modificadas no working tree"
			if len(f.cands) == 0 {
				label = "Nenhuma função Go modificada: digite para buscar em todo o módulo"
			}
		} else if len(f.cands) > 1 {
			exact := 0
			for _, p := range f.progs {
				exact += len(p.Find(f.input.Value()))
			}
			if exact > 1 {
				label = fmt.Sprintf("%d símbolos com esse nome · escolha exatamente um", exact)
			}
		}
		lines = append(lines, "  "+stSubtle.Render(label), "")
		per := 3
		avail := max((h-len(lines))/per, 1)
		start := max(f.csel-avail+1, 0)
		for i := start; i < len(f.cands) && i-start < avail; i++ {
			c := f.cands[i].sym
			pointer, st := "   ", stText
			if i == f.csel {
				pointer, st = " "+stAccent.Render(sym.closed)+" ", stBold
			}
			lines = append(lines,
				pointer+stMuted.Render(ansiTruncate(c.Pkg, w-4)),
				"   "+st.Render(ansiTruncate(c.Qualified, w-4)),
				"   "+stSubtle.Render(fmt.Sprintf("%s:%d", c.File, c.Line)))
		}
	}
	if f.msg != "" {
		lines = append(lines, "", "  "+stDel.Render(ansiTruncate(f.msg, w-3)))
	}
	return strings.Join(lines, "\n")
}

var confStyle = map[string]lipgloss.Style{
	flow.Confirmed: stText, flow.Likely: stSubtle, flow.Possible: stMuted, flow.Unresolved: stMod,
}

func (m *model) flowRowView(r flowRow, selected bool, w int) string {
	f := m.flow
	if r.header != "" {
		return " " + stSubtle.Bold(true).Render(r.header)
	}
	n := f.graph.Node(r.id)
	bg := lipgloss.NewStyle()
	if selected {
		bg = bg.Background(colSelBg)
	}
	with := func(st lipgloss.Style) lipgloss.Style { return bg.Inherit(st) }
	marker := bg.Render(" ")
	if selected {
		marker = with(stAccent).Render(sym.bar)
	}
	prefix := ""
	nameSt := stText
	if r.edge == nil {
		prefix, nameSt = "● ", stTitle
	} else {
		arrow := "→ "
		if r.up {
			arrow = "← "
		}
		switch r.edge.Kind {
		case flow.EdgeGo:
			arrow += "go "
		case flow.EdgeDefer:
			arrow += "defer "
		}
		prefix = strings.Repeat("  ", r.indent) + arrow
		nameSt = confStyle[r.edge.Confidence]
	}
	switch n.Kind {
	case flow.KindRoute:
		nameSt = stAccent
	case flow.KindDB:
		nameSt = stAdd
	case flow.KindExternal:
		nameSt = stMuted
	case flow.KindUnresolved:
		nameSt = stMod
	}
	var tail []string
	if r.edge != nil && r.edge.Confidence != flow.Confirmed {
		tail = append(tail, r.edge.Confidence)
	}
	if n.Changed {
		tail = append(tail, "modificada")
	}
	if n.Recursive || r.cycle {
		tail = append(tail, "ciclo")
	}
	if n.SQLDynamic {
		tail = append(tail, "SQL dinâmico")
	}
	loc := ""
	if n.File != "" {
		loc = fmt.Sprintf("%s:%d", n.File, n.Line)
	} else if n.Kind == flow.KindExternal {
		loc = n.Pkg
	}
	left := marker + with(stMuted).Render(prefix) + with(nameSt.Inherit(bg)).Render(n.Name)
	if len(tail) > 0 {
		left += with(stMuted).Render("  " + strings.Join(tail, " · "))
	}
	locW := runewidth.StringWidth(loc)
	if lipgloss.Width(left)+locW+3 > w {
		left = ansiTruncate(left, max(w-locW-3, 10))
	}
	gap := max(w-lipgloss.Width(left)-locW-1, 1)
	return left + bg.Render(strings.Repeat(" ", gap)) + with(stSubtle).Render(loc) + bg.Render(" ")
}

// flowDetail shows the selected node's signature and explanation.
func (m *model) flowDetail(w int) []string {
	f := m.flow
	r := f.selectedRow()
	if r == nil {
		return nil
	}
	n := f.graph.Node(r.id)
	out := []string{" " + stBorder.Render(strings.Repeat("─", max(w-2, 1)))}
	if n.Signature != "" {
		out = append(out, " "+stText.Render(ansiTruncate(n.Signature, w-2)))
	}
	text := n.Explain
	if n.SQL != "" {
		text = "SQL: " + cleanText(strings.Join(strings.Fields(n.SQL), " ")) + "  " + text
	}
	for _, l := range wrap(text, w-2, 3) {
		out = append(out, " "+stSubtle.Render(l))
	}
	if f.msg != "" {
		out = append(out, " "+stMod.Render(ansiTruncate(f.msg, w-2)))
	}
	return out
}

func wrap(s string, w, maxLines int) []string {
	var lines []string
	cur := ""
	for _, word := range strings.Fields(s) {
		if runewidth.StringWidth(cur)+runewidth.StringWidth(word)+1 > w && cur != "" {
			lines = append(lines, cur)
			cur = ""
			if len(lines) == maxLines {
				lines[maxLines-1] = ansiTruncate(lines[maxLines-1]+" "+word, w)
				return lines
			}
		}
		if cur != "" {
			cur += " "
		}
		cur += word
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func (m *model) flowFooter() string {
	f := m.flow
	if f.phase == phasePick {
		return renderHints(m.width, []keyHint{{sym.updown, "Escolher"}, {"Enter", "Analisar"}, {"Esc", "Voltar"}}, 2)
	}
	if m.insp.tab != tabCodeFlow {
		return m.inspFooter()
	}
	return renderHints(m.width, []keyHint{{"1-6", "Abas"},
		{sym.updown, "Navegar"}, {"Enter", "Set as root"}, {"Tab", f.dir}, {"+/-", "Profundidade"},
		{"g", "Ir ao diff"}, {"/", "Buscar"}, {"?", "Ajuda"}, {"Esc", "Voltar"},
	}, 2)
}

var flowHelp = []keyHint{
	{"1-6  [  ]", "abas do Go Deep Inspector (mesma função em todas)"},
	{"digitar", "Find Function: nome, pacote, receiver, arquivo"},
	{"↑ ↓  Enter", "escolher o símbolo exato (homônimos aparecem separados)"},
	{"Tab", "direção: both → callers → callees"},
	{"+ -", "profundidade (1 a 5, em cada direção)"},
	{"Enter", "usar a função selecionada como raiz (Set as root)"},
	{"Backspace", "voltar à raiz anterior"},
	{"g", "abrir a definição no diff, se modificada"},
	{"/", "nova busca"},
	{"Esc", "voltar"},
}
