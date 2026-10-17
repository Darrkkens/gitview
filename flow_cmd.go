package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/gitdiff"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
	"golang.org/x/tools/go/ssa"
)

const flowUsage = `uso: gitview flow [opções] [diretório]

Mostra o fluxo de chamadas de funções Go (análise estática com go/packages,
go/types e SSA). Sem --function, usa as funções modificadas no working tree.

exemplos:
  gitview flow --function GetClientByID --direction both --depth 5
  gitview flow --function 'client.(*ClientService).GetClientByID' --direction callers
  gitview flow --function service/client.go:84
  gitview flow --find Client            # busca símbolos (autocomplete)
  gitview flow --routes                 # rotas HTTP e seus fluxos
  gitview flow --function GetClientByID --json > flow.json

opções:
`

type flowFlags struct {
	function   string
	direction  string
	depth      int
	routes     bool
	find       string
	jsonOut    bool
	noExternal bool
	maxNodes   int
	nonInter   bool
}

// runFlow implements `gitview flow`. It returns the process exit code.
func runFlow(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("flow", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var f flowFlags
	fs.StringVar(&f.function, "function", "", "função a analisar: nome, nome qualificado, ID completo ou arquivo:linha")
	fs.StringVar(&f.function, "f", "", "atalho para --function")
	fs.StringVar(&f.direction, "direction", flow.Both, "callers, callees ou both")
	fs.IntVar(&f.depth, "depth", 5, "profundidade máxima em cada direção (1-10)")
	fs.BoolVar(&f.routes, "routes", false, "mostra as rotas HTTP encontradas e seus fluxos")
	fs.StringVar(&f.find, "find", "", "lista símbolos cujo nome contém o texto")
	fs.BoolVar(&f.jsonOut, "json", false, "imprime o grafo em JSON")
	fs.BoolVar(&f.noExternal, "no-external", false, "oculta chamadas a bibliotecas externas")
	fs.IntVar(&f.maxNodes, "max-nodes", 400, "limite de nós do grafo")
	fs.BoolVar(&f.nonInter, "non-interactive", false, "nunca abre o seletor; homônimos geram erro de ambiguidade")
	fs.Usage = func() { fmt.Fprint(stderr, flowUsage); fs.PrintDefaults() }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if f.depth < 1 || f.depth > 10 {
		fmt.Fprintln(stderr, "gitview flow: --depth deve estar entre 1 e 10")
		return 2
	}
	if f.direction != flow.Both && f.direction != flow.Callers && f.direction != flow.Callees {
		fmt.Fprintln(stderr, "gitview flow: --direction deve ser callers, callees ou both")
		return 2
	}
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	root, progs, err := loadPrograms(ctx, dir, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "gitview flow:", err)
		return 1
	}
	opt := flow.Options{Depth: f.depth, MaxNodes: f.maxNodes, External: !f.noExternal, Routes: true}

	var g *flow.Graph
	switch {
	case f.find != "":
		printSymbols(stdout, progs, f.find)
		return 0
	case f.routes:
		g = &flow.Graph{}
		for _, p := range progs {
			g.Merge(p.RoutesFlow(opt))
		}
	case f.function != "":
		p, sym, err := pickSymbol(progs, f.function, !f.nonInter && isTerminal(), stderr)
		if err != nil {
			fmt.Fprintln(stderr, "gitview flow:", err)
			var amb *flow.AmbiguityError
			if errors.As(err, &amb) {
				return 3
			}
			return 1
		}
		if p == nil {
			return 130 // selector cancelled
		}
		fn, _ := p.Resolve(sym.ID)
		if g, err = p.Query(fn, f.direction, opt); err != nil {
			fmt.Fprintln(stderr, "gitview flow:", err)
			return 1
		}
	default:
		changed, err := changedLines(root)
		if err != nil {
			fmt.Fprintln(stderr, "gitview flow:", err)
			return 1
		}
		g = &flow.Graph{}
		for _, p := range progs {
			opt.Changed = changed
			var fns []string
			for _, c := range p.ChangedFunctions(changed) {
				fns = append(fns, c.ID)
			}
			if len(fns) == 0 {
				continue
			}
			var roots = resolveAll(p, fns)
			g.Merge(p.Flow(roots, "changed", opt))
		}
		if len(g.Roots) == 0 {
			fmt.Fprintln(stderr, "Nenhuma função Go modificada no working tree. Use --function NOME.")
			return 0
		}
	}

	if f.jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(g); err != nil {
			return 1
		}
		return 0
	}
	printFlow(stdout, g, f.direction)
	return 0
}

func isTerminal() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stderr.Fd())
}

// loadPrograms analyses the module containing dir, or every module of the
// repository when dir is not inside one.
func loadPrograms(ctx context.Context, dir string, stderr io.Writer) (string, []*flow.Program, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, err
	}
	root := abs
	if repo, err := OpenRepo(abs); err == nil {
		root = repo.Root
	}
	mods := flow.FindModules(root)
	if m := flow.EnclosingModule(abs, root); m != "" {
		mods = []string{m}
	}
	if len(mods) == 0 {
		return "", nil, fmt.Errorf("nenhum módulo Go (go.mod) em %s", root)
	}
	var progs []*flow.Program
	for _, m := range mods {
		start := time.Now()
		rel, _ := filepath.Rel(root, m)
		fmt.Fprintf(stderr, "analisando %s…", rel)
		p, err := flow.Load(ctx, root, m)
		if err != nil {
			fmt.Fprintln(stderr)
			return "", nil, fmt.Errorf("%s: %w", rel, err)
		}
		fmt.Fprintf(stderr, " %s, %d funções, %d rotas", time.Since(start).Round(10*time.Millisecond), len(p.Symbols()), p.RouteCount())
		if len(p.Errors) > 0 {
			fmt.Fprintf(stderr, ", %d erro(s) de compilação: resultados parciais", len(p.Errors))
		}
		fmt.Fprintln(stderr)
		progs = append(progs, p)
	}
	return root, progs, nil
}

func resolveAll(p *flow.Program, ids []string) []*ssa.Function {
	var out []*ssa.Function
	for _, id := range ids {
		if fn, err := p.Resolve(id); err == nil {
			out = append(out, fn)
		}
	}
	return out
}

// changedLines maps repository files to the new-side lines changed in the
// working tree, from the structured diff.
func changedLines(root string) (map[string]map[int]bool, error) {
	repo, err := OpenRepo(root)
	if err != nil {
		return nil, err
	}
	files, err := repo.Changes()
	if err != nil {
		return nil, err
	}
	return changedFromFiles(files, func(f FileChange) *parsedDiff { return parseDiff(f.Path, f.Diff) }), nil
}

func changedFromFiles(files []FileChange, parse func(FileChange) *parsedDiff) map[string]map[int]bool {
	out := map[string]map[int]bool{}
	for _, f := range files {
		if !strings.HasSuffix(f.Path, ".go") {
			continue
		}
		pd := parse(f)
		if pd == nil {
			continue
		}
		lines := map[int]bool{}
		for _, h := range pd.hunks {
			next := h.NewStart
			for _, l := range h.Lines {
				switch {
				case l.Kind == gitdiff.Added:
					lines[l.NewNum] = true
					next = l.NewNum + 1
				case l.NewNum > 0:
					next = l.NewNum + 1
				default: // removed: mark the position where it was
					lines[next] = true
				}
			}
		}
		out[f.Path] = lines
	}
	return out
}

// pickSymbol finds the symbol to analyse. Ambiguous names open a selector
// when interactive, and fail with the candidates otherwise.
func pickSymbol(progs []*flow.Program, q string, interactive bool, stderr io.Writer) (*flow.Program, flow.Symbol, error) {
	type cand struct {
		p *flow.Program
		s flow.Symbol
	}
	var all []cand
	var syms []flow.Symbol
	for _, p := range progs {
		for _, s := range p.Find(q) {
			all = append(all, cand{p, s})
			syms = append(syms, s)
		}
	}
	switch {
	case len(all) == 0:
		return nil, flow.Symbol{}, fmt.Errorf("nenhuma função corresponde a %q", q)
	case len(all) == 1:
		return all[0].p, all[0].s, nil
	case !interactive:
		return nil, flow.Symbol{}, &flow.AmbiguityError{Query: q, Candidates: syms}
	}
	i, err := runSelector(q, syms, stderr)
	if err != nil || i < 0 {
		return nil, flow.Symbol{}, err
	}
	return all[i].p, all[i].s, nil
}

func printSymbols(w io.Writer, progs []*flow.Program, text string) {
	text = strings.ToLower(text)
	n := 0
	for _, p := range progs {
		for _, s := range p.Symbols() {
			if strings.Contains(strings.ToLower(s.Qualified), text) {
				fmt.Fprintf(w, "%-60s %s:%d\n", s.Qualified, s.File, s.Line)
				n++
			}
		}
	}
	if n == 0 {
		fmt.Fprintln(w, "nenhum símbolo encontrado")
	}
}

// --- text output ---------------------------------------------------------------

var confMark = map[string]string{
	flow.Confirmed: "", flow.Likely: " [provável]", flow.Possible: " [possível]", flow.Unresolved: " [não resolvida]",
}

func nodeLine(n *flow.Node) string {
	loc := ""
	if n.File != "" {
		loc = fmt.Sprintf("  %s:%d", n.File, n.Line)
	}
	extra := ""
	switch n.Kind {
	case flow.KindExternal:
		extra = "  (externa: " + n.Pkg + ")"
	case flow.KindDB:
		switch {
		case n.SQL != "":
			extra = "  SQL: " + oneLine(n.SQL, 70)
		case n.SQLDynamic:
			extra = "  SQL dinâmico"
		}
	case flow.KindRoute:
		extra = "  (" + n.Router + ")"
		if n.Partial {
			extra += " parcial: " + n.Note
		}
	case flow.KindUnresolved:
		extra = "  " + n.Note
	}
	var tags []string
	if n.Changed {
		tags = append(tags, "modificada")
	}
	if n.Recursive {
		tags = append(tags, "recursiva")
	}
	tags = append(tags, n.Tags...)
	if len(tags) > 0 {
		extra += "  {" + strings.Join(tags, ", ") + "}"
	}
	return n.Name + loc + extra
}

func oneLine(s string, n int) string {
	s = cleanText(strings.Join(strings.Fields(s), " "))
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func edgeArrow(e *flow.Edge, up bool) string {
	a := "→"
	if up {
		a = "←"
	}
	switch e.Kind {
	case flow.EdgeGo:
		a += " go"
	case flow.EdgeDefer:
		a += " defer"
	}
	return a
}

func printFlow(w io.Writer, g *flow.Graph, direction string) {
	for _, r := range g.Roots {
		root := g.Node(r.ID)
		if root == nil {
			continue
		}
		fmt.Fprintf(w, "\n%s\n", nodeLine(root))
		if root.Signature != "" {
			fmt.Fprintf(w, "  %s\n", root.Signature)
		}
		if direction != flow.Callees && r.Kind != "route" {
			fmt.Fprintf(w, "\nCallers (profundidade ≤ %d)\n", g.MaxDepth)
			printTree(w, g, r.ID, true, "  ", map[string]bool{r.ID: true})
		}
		if direction != flow.Callers || r.Kind == "route" {
			fmt.Fprintf(w, "\nCallees (profundidade ≤ %d)\n", g.MaxDepth)
			printTree(w, g, r.ID, false, "  ", map[string]bool{r.ID: true})
		}
	}
	if g.Truncated {
		fmt.Fprintln(w, "\n… limites de profundidade ou de nós atingidos: parte do grafo não foi explorada.")
	}
	for _, n := range g.Notes {
		fmt.Fprintln(w, "nota:", n)
	}
	fmt.Fprintln(w, "\nAnálise estática: as arestas são caminhos possíveis, não chamadas observadas em execução.")
}

func printTree(w io.Writer, g *flow.Graph, id string, up bool, indent string, path map[string]bool) {
	edges := g.Out(id)
	if up {
		edges = g.In(id)
	}
	if len(edges) == 0 && indent == "  " {
		fmt.Fprintln(w, indent+"(nenhuma)")
	}
	sort.SliceStable(edges, func(i, j int) bool { return edges[i].Order < edges[j].Order })
	for _, e := range edges {
		other := e.To
		if up {
			other = e.From
		}
		n := g.Node(other)
		if n == nil {
			continue
		}
		line := indent + edgeArrow(e, up) + " " + nodeLine(n) + confMark[e.Confidence]
		if path[other] {
			fmt.Fprintln(w, line+"  ↻ ciclo")
			continue
		}
		fmt.Fprintln(w, line)
		path[other] = true
		printTree(w, g, other, up, indent+"   ", path)
		delete(path, other)
	}
}

// --- interactive selector ------------------------------------------------------

type selectorModel struct {
	query  string
	all    []flow.Symbol
	shown  []int
	sel    int
	filter textinput.Model
	chosen int
	width  int
	height int
}

func runSelector(q string, syms []flow.Symbol, out io.Writer) (int, error) {
	ti := textinput.New()
	ti.Placeholder = "filtrar por pacote, receiver ou arquivo"
	ti.Width = 48
	ti.Prompt = "  / "
	ti.PromptStyle = stAccent
	ti.Focus()
	m := &selectorModel{query: q, all: syms, filter: ti, chosen: -1}
	m.apply()
	res, err := tea.NewProgram(m, tea.WithOutput(out)).Run()
	if err != nil {
		return -1, err
	}
	return res.(*selectorModel).chosen, nil
}

func (m *selectorModel) Init() tea.Cmd { return textinput.Blink }

func (m *selectorModel) apply() {
	f := strings.ToLower(m.filter.Value())
	m.shown = m.shown[:0]
	for i, s := range m.all {
		hay := strings.ToLower(s.Pkg + " " + s.Recv + " " + s.Qualified + " " + s.File)
		if f == "" || strings.Contains(hay, f) {
			m.shown = append(m.shown, i)
		}
	}
	m.sel = min(m.sel, max(len(m.shown)-1, 0))
}

func (m *selectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "enter":
			if len(m.shown) > 0 {
				m.chosen = m.shown[m.sel]
			}
			return m, tea.Quit
		case "up", "ctrl+p", "shift+tab":
			m.sel = max(m.sel-1, 0)
			return m, nil
		case "down", "ctrl+n", "tab":
			m.sel = min(m.sel+1, max(len(m.shown)-1, 0))
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.apply()
		return m, cmd
	}
	return m, nil
}

func (m *selectorModel) View() string {
	var b strings.Builder
	b.WriteString("\n " + stTitle.Render("Find Function: ") + stText.Render(m.query) + "\n\n")
	b.WriteString(" " + stSubtle.Render(fmt.Sprintf("%d símbolos encontrados", len(m.all))) + "\n\n")
	b.WriteString(m.filter.View() + "\n\n")
	for i, idx := range m.shown {
		s := m.all[idx]
		pointer, st := "   ", stText
		if i == m.sel {
			pointer, st = " "+stAccent.Render(sym.closed)+" ", stBold
		}
		b.WriteString(pointer + stMuted.Render(s.Pkg) + "\n")
		b.WriteString("   " + st.Render(s.Qualified) + "\n")
		b.WriteString("   " + stSubtle.Render(fmt.Sprintf("%s:%d", s.File, s.Line)) + "\n\n")
	}
	if len(m.shown) == 0 {
		b.WriteString("   " + stMuted.Render("nenhum símbolo corresponde ao filtro") + "\n\n")
	}
	b.WriteString(" " + stText.Render("Enter") + " " + stMuted.Render("Selecionar") + "   " + stText.Render("Esc") + " " + stMuted.Render("Cancelar") + "\n")
	return lipgloss.NewStyle().Render(b.String())
}
