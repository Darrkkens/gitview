package main

// Text rendering of the Go Deep Inspector views, shared by the CLI (plain
// text) and the TUI (styled, with selectable lines). Every line carries
// only facts from the analyses; wording marks static, observed and
// estimated results.

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/inspect"
	"github.com/athos/gitview/internal/inspect/branch"
	"github.com/athos/gitview/internal/inspect/dataflow"
	"github.com/athos/gitview/internal/inspect/perf"
	"github.com/athos/gitview/internal/inspect/pgexplain"
	"github.com/athos/gitview/internal/inspect/sqlops"
	"github.com/athos/gitview/internal/inspect/xtrace"
	"github.com/charmbracelet/lipgloss"
)

// Span styles.
const (
	sPlain  = ""
	sTitle  = "title"
	sBold   = "bold"
	sMuted  = "muted"
	sSubtle = "subtle"
	sAccent = "accent"
	sGood   = "good"
	sBad    = "bad"
	sWarn   = "warn"
)

type tspan struct{ text, st string }

type docLine struct {
	indent int
	spans  []tspan
	ref    string // selectable item (TUI), e.g. "b:3", "n:12", "op:0"
	loc    string // file:line for navigation
}

type doc []docLine

func (d *doc) add(indent int, spans ...tspan) { *d = append(*d, docLine{indent: indent, spans: spans}) }

func (d *doc) item(indent int, ref, loc string, spans ...tspan) {
	*d = append(*d, docLine{indent: indent, spans: spans, ref: ref, loc: loc})
}

func (d *doc) blank() { *d = append(*d, docLine{}) }

func sp(text string) tspan      { return tspan{text, sPlain} }
func spS(text, st string) tspan { return tspan{text, st} }
func loc(file string, line int) string {
	if file == "" {
		return ""
	}
	if line <= 0 {
		return file
	}
	return fmt.Sprintf("%s:%d", file, line)
}

// cleanText neutralises control characters coming from the analysed code
// or the monitored process (SQL literals, trace logs, profile names, error
// replies) so they cannot drive the terminal: they are shown as \xNN (or
// \u{NNNN} for bidirectional overrides, which reorder what is displayed).
// Bytes that are not valid UTF-8 are shown the same way.
func cleanText(s string) string {
	clean := utf8.ValidString(s)
	for _, r := range s {
		if !clean || unsafeRune(r) {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size <= 1:
			fmt.Fprintf(&b, "\\x%02x", s[i])
		case r == '\t':
			b.WriteString("    ")
		case r < 0x20 || r == 0x7f || r >= 0x80 && r < 0xa0:
			fmt.Fprintf(&b, "\\x%02x", r)
		case unsafeRune(r):
			fmt.Fprintf(&b, "\\u{%04x}", r)
		default:
			b.WriteRune(r)
		}
		i += size
	}
	return b.String()
}

// unsafeRune: control characters and bidirectional formatting characters.
func unsafeRune(r rune) bool {
	return r < 0x20 || r == 0x7f || r >= 0x80 && r < 0xa0 ||
		r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 || r == 0x200e || r == 0x200f || r == 0x061c
}

// plain renders a document as text for the terminal (CLI).
func (d doc) plain() string {
	var b strings.Builder
	for _, l := range d {
		b.WriteString(strings.Repeat("  ", l.indent))
		for _, s := range l.spans {
			b.WriteString(cleanText(s.text))
		}
		if l.loc != "" {
			b.WriteString("   " + cleanText(l.loc))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

var docStyles = map[string]lipgloss.Style{
	sPlain: stText, sTitle: stTitle, sBold: stBold, sMuted: stMuted, sSubtle: stSubtle,
	sAccent: stAccent, sGood: stAdd, sBad: stDel, sWarn: stMod,
}

// styled renders one line for the TUI.
func (l docLine) styled(width int, selected bool) string {
	bg := lipgloss.NewStyle()
	if selected {
		bg = bg.Background(colSelBg)
	}
	marker := bg.Render(" ")
	if selected {
		marker = bg.Inherit(stAccent).Render(sym.bar)
	}
	left := marker + bg.Render(strings.Repeat("  ", l.indent))
	for _, s := range l.spans {
		left += bg.Inherit(docStyles[s.st]).Render(cleanText(s.text))
	}
	l.loc = cleanText(l.loc)
	locW := lipgloss.Width(l.loc)
	if lipgloss.Width(left)+locW+3 > width {
		left = ansiTruncate(left, max(width-locW-3, 10))
	}
	gap := max(width-lipgloss.Width(left)-locW-1, 1)
	return left + bg.Render(strings.Repeat(" ", gap)) + bg.Inherit(stSubtle).Render(l.loc) + bg.Render(" ")
}

func confStyle2(conf string) string {
	switch conf {
	case evidence.Exact:
		return sPlain
	case evidence.Derived:
		return sSubtle
	case evidence.Likely, evidence.Possible:
		return sMuted
	}
	return sWarn
}

func statusLine(d *doc, c evidence.Completeness) {
	switch c.Status {
	case evidence.Complete, "":
	default:
		d.add(0, spS("Resultado "+statusPT(c.Status)+": ", sWarn), spS(strings.Join(c.Reasons, "; "), sSubtle))
	}
}

func symbolHeader(d *doc, view string, ref evidence.SymbolRef, extra string) {
	d.add(0, spS(view, sTitle), sp("  "), spS(ref.Qualified, sBold), spS("  "+loc(ref.File, ref.Line), sSubtle))
	if extra != "" {
		d.add(0, spS(extra, sMuted))
	}
}

// --- Branch Explorer ------------------------------------------------------------

func coverMark(state string) tspan {
	switch state {
	case branch.Covered:
		return spS(" ● coberto", sGood)
	case branch.Uncovered:
		return spS(" ○ não coberto", sBad)
	case branch.PartCover:
		return spS(" ◐ parcialmente coberto", sWarn)
	case branch.Unknown:
		return spS(" ? cobertura desconhecida", sMuted)
	}
	return tspan{}
}

func renderBranches(g *branch.Graph, showPaths bool) doc {
	var d doc
	m := g.Metrics
	symbolHeader(&d, "Branch Explorer", g.Symbol, "Grafo de fluxo de controle construído a partir da sintaxe (golang.org/x/tools/go/cfg): caminhos possíveis, não observados.")
	d.add(0, spS(fmt.Sprintf("%d blocos · %d arestas · %d decisões · complexidade ciclomática %d · %d retornos · %d panic/exit · %d laços · %d caminhos",
		m.Blocks, m.Edges, m.DecisionPoints, m.Cyclomatic, m.Returns, m.Panics, m.Loops, m.Paths), sSubtle))
	if m.Unreachable > 0 {
		d.add(0, spS(fmt.Sprintf("%d bloco(s) inalcançável(is) (código morto)", m.Unreachable), sWarn))
	}
	if c := g.Coverage; c != nil {
		d.add(0, spS(fmt.Sprintf("Cobertura (%s, modo %s): %d cobertos · %d não cobertos · %d parciais · %d desconhecidos", c.Profile, c.Mode, c.Covered, c.Uncovered, c.Partial, c.Unknown), sAccent))
		d.add(0, spS(c.Note, sMuted))
	}
	if len(g.Defers) > 0 {
		var ds []string
		for _, s := range g.Defers {
			ds = append(ds, fmt.Sprintf("%s (linha %d)", s.Text, s.Line))
		}
		d.add(0, spS("Chamadas adiadas (defer) rodam em toda saída posterior ao registro: ", sSubtle), sp(strings.Join(ds, "; ")))
	}
	d.blank()
	for _, r := range g.Outline() {
		b := g.Block(r.Block)
		var spans []tspan
		if r.Label != "" {
			st := sAccent
			if r.Error {
				st = sBad
			}
			spans = append(spans, spS(r.Label+" → ", st))
		}
		switch {
		case r.Loop:
			spans = append(spans, spS(fmt.Sprintf("↻ volta a B%d (%s)", b.ID, b.Summary()), sMuted))
			d.item(r.Depth, fmt.Sprintf("b:%d", b.ID), loc(b.File, b.Line), spans...)
			continue
		case r.Join:
			spans = append(spans, spS(fmt.Sprintf("segue em B%d", b.ID), sMuted))
			d.item(r.Depth, fmt.Sprintf("b:%d", b.ID), loc(b.File, b.Line), spans...)
			continue
		case r.Ref:
			spans = append(spans, spS(fmt.Sprintf("B%d %s", b.ID, b.Summary()), sMuted), spS("  (mostrado em outro ponto)", sMuted))
			d.item(r.Depth, fmt.Sprintf("b:%d", b.ID), loc(b.File, b.Line), spans...)
			continue
		}
		spans = append(spans, spS(fmt.Sprintf("B%d ", b.ID), sMuted))
		st := sPlain
		switch {
		case b.Decision != "":
			st = sBold
		case b.Terminal == branch.TermPanic || b.Terminal == branch.TermExit:
			st = sBad
		case !b.Reachable:
			st = sMuted
		}
		spans = append(spans, spS(b.Summary(), st))
		var tags []string
		if b.Entry {
			tags = append(tags, "entrada")
		}
		if b.LoopHead {
			tags = append(tags, "início de laço")
		}
		if b.ErrorCheck {
			tags = append(tags, "verifica erro")
		}
		if b.ReturnsError {
			tags = append(tags, "retorna erro")
		}
		if b.Terminal != "" && b.Terminal != branch.TermReturn {
			tags = append(tags, terminalPT(b.Terminal))
		}
		if !b.Reachable {
			tags = append(tags, "inalcançável")
		}
		if len(tags) > 0 {
			spans = append(spans, spS("  ["+strings.Join(tags, ", ")+"]", sMuted))
		}
		if b.Coverage != "" {
			spans = append(spans, coverMark(b.Coverage))
		}
		d.item(r.Depth, fmt.Sprintf("b:%d", b.ID), loc(b.File, b.Line), spans...)
		for _, s := range extraStmts(b) {
			d.add(r.Depth+2, spS(s.Text, sMuted))
		}
	}
	if showPaths {
		d.blank()
		d.add(0, spS(fmt.Sprintf("Caminhos até uma saída (%d de %d listados; cada laço repetido no máximo uma vez)", m.PathsListed, m.Paths), sBold))
		for i, p := range g.Paths {
			steps := strings.Join(p.Steps, "  ·  ")
			if steps == "" {
				steps = "(sem decisões)"
			}
			end := g.Block(p.End)
			st := sPlain
			if p.Error {
				st = sBad
			}
			var ids []string
			for _, b := range p.Blocks {
				ids = append(ids, fmt.Sprint("B", b))
			}
			d.item(1, fmt.Sprintf("p:%d", i), loc(end.File, end.Line), spS(fmt.Sprintf("%d. ", i+1), sMuted), sp(steps), spS("  ⇒ "+end.Summary(), st), spS("  ("+strings.Join(ids, " → ")+")", sMuted))
		}
	}
	d.blank()
	for _, n := range g.Notes {
		d.add(0, spS(n, sMuted))
	}
	statusLine(&d, g.Status)
	return d
}

// --- Data Flow ----------------------------------------------------------------------

func nodeSpans(n *dataflow.Node) []tspan {
	st := sPlain
	switch n.Kind {
	case dataflow.KindSource:
		st = sAccent
	case dataflow.KindSQL:
		st = sGood
	case dataflow.KindCheck:
		st = sWarn
	case dataflow.KindUnresolved:
		st = sWarn
	case dataflow.KindExternal, dataflow.KindReturn:
		st = sSubtle
	}
	prefix := ""
	switch n.Kind {
	case dataflow.KindSource:
		prefix = "fonte "
	case dataflow.KindCheck:
		prefix = "✓ verificação "
	case dataflow.KindSQL:
		prefix = "⛁ "
	case dataflow.KindUnresolved:
		prefix = "? "
	}
	spans := []tspan{spS(prefix+n.Label, st)}
	if n.Detail != "" && n.Kind != dataflow.KindCheck {
		spans = append(spans, spS("  "+n.Detail, sMuted))
	}
	if n.FuncName != "" && n.Level != 0 {
		spans = append(spans, spS("  em "+n.FuncName, sSubtle))
	}
	return spans
}

func edgeSpans(e *dataflow.Edge, origin bool) []tspan {
	arrow := "→ "
	if origin {
		arrow = "⇐ "
	}
	label := e.Kind
	switch {
	case e.Kind == dataflow.EdgeSink || e.Kind == dataflow.EdgeSource:
		label = e.Label
	case e.Label != "":
		label += ": " + e.Label
	}
	return []tspan{spS(arrow, sMuted), spS("["+e.Conf+"] ", confStyle2(e.Conf)), spS(label+"  ", sMuted)}
}

func renderDataFlow(r *dataflow.Result, hideIntermediate bool) doc {
	if hideIntermediate {
		r = r.HideIntermediate()
	}
	var d doc
	sel := r.Selection
	what := sel.Name
	if sel.Type != "" {
		what += " (" + sel.Type + ")"
	}
	symbolHeader(&d, "Data Flow", r.Symbol, fmt.Sprintf("Rastreando %s %s · direção %s · até %d nível(is) de chamada · análise estática (SSA): fluxos possíveis, não valores observados.", selKindPT(sel.Kind), what, dirPT(r.Direction), r.MaxDepth))
	for _, dir := range []dataflow.Direction{dataflow.Origin, dataflow.Destination} {
		if r.Direction != dataflow.Both && r.Direction != dir {
			continue
		}
		d.blank()
		title := "Origem — de onde o valor vem"
		if dir == dataflow.Destination {
			title = "Destino — para onde o valor pode ir"
		}
		d.add(0, spS(title, sBold))
		rows := r.Tree(dir)
		if len(rows) <= len(r.Roots) {
			d.add(1, spS("(nada encontrado nesta direção)", sMuted))
		}
		for _, row := range rows {
			n := r.Node(row.Node)
			var spans []tspan
			if row.Edge != nil {
				spans = append(spans, edgeSpans(row.Edge, dir == dataflow.Origin)...)
			}
			if row.Ref {
				spans = append(spans, spS(n.Label+"  (ver acima)", sMuted))
			} else {
				spans = append(spans, nodeSpans(n)...)
			}
			d.item(row.Depth+1, fmt.Sprintf("n:%d", n.ID), loc(n.File, n.Line), spans...)
		}
	}
	if len(r.Validations) > 0 {
		d.blank()
		d.add(0, spS(fmt.Sprintf("Validações (%d): condições sobre o valor, ou sobre valores derivados dele, que decidem um desvio", len(r.Validations)), sBold))
		for _, v := range r.Validations {
			d.item(1, fmt.Sprintf("n:%d", v.Node), loc(v.File, v.Line), spS(v.Text, sWarn), spS("  "+v.Kind+" em "+v.Func, sMuted))
		}
	}
	var srcs []string
	for _, id := range r.Sources {
		srcs = append(srcs, r.Node(id).Label)
	}
	if len(srcs) > 0 {
		d.blank()
		d.add(0, spS("Fontes: ", sBold), sp(strings.Join(uniq(srcs), "; ")))
	}
	d.blank()
	for _, n := range r.Notes {
		d.add(0, spS(n, sMuted))
	}
	statusLine(&d, r.Status)
	return d
}

func uniq(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// --- SQL Explorer -----------------------------------------------------------------

func renderSQL(r *sqlops.Result, plans []*pgexplain.Plan) doc {
	var d doc
	scope := "das operações feitas diretamente pela função"
	if r.Reachable {
		scope = fmt.Sprintf("das operações da função e das funções do módulo que ela pode chamar (profundidade ≤ %d)", r.Depth)
	}
	symbolHeader(&d, "SQL Explorer", r.Symbol, "Descoberta estática "+scope+" (pgx, pgxpool, database/sql, sqlx). Nada foi executado.")
	if len(r.Ops) == 0 {
		d.blank()
		d.add(1, spS(inspect.NoSQL, sMuted))
	}
	for i, op := range r.Ops {
		d.blank()
		conf := ""
		if op.Conf != evidence.Exact {
			conf = " [" + op.Conf + "]"
		}
		d.item(0, fmt.Sprintf("op:%d", i), loc(op.File, op.Line), spS(fmt.Sprintf("%d. ", i+1), sMuted), spS(op.Lib+" "+op.Op, sGood),
			spS("  "+op.Method+" on "+op.Receiver, sSubtle), spS(conf, sMuted), spS("  em "+op.Func.Qualified, sSubtle))
		if len(op.Via) > 0 {
			d.add(2, spS("alcançada via "+strings.Join(op.Via, " → "), sMuted))
		}
		if op.Deferred {
			d.add(2, spS("adiada (defer): roda quando a função retorna", sMuted))
		}
		if op.Call != "" {
			d.add(2, spS(op.Call, sSubtle))
		}
		renderSQLText(&d, op.SQL)
		if len(op.Params) > 0 {
			d.add(2, spS("Parâmetros:", sBold))
			for _, p := range op.Params {
				d.add(3, spS(fmt.Sprintf("$%d", p.Index), sAccent), sp(" = "+p.Expr), spS("  "+p.Type, sMuted))
				for _, o := range p.Origins {
					st := sSubtle
					if o.Kind == "source" {
						st = sAccent
					}
					d.add(4, spS("⇐ ", sMuted), spS(o.Label, st), spS(fmt.Sprintf("  [%s]  %s", o.Conf, loc(o.File, o.Line)), sMuted))
				}
				if p.OriginStatus == string(evidence.Partial) {
					d.add(4, spS("(a busca da origem parou nos limites)", sMuted))
				}
			}
		}
		if !op.ParamsOK {
			d.add(2, spS("Parâmetros passados como slice: desconhecidos estaticamente.", sWarn))
		}
		if s := op.Scan; s != nil {
			var ts []string
			for _, t := range s.Targets {
				ts = append(ts, t.Expr)
			}
			what := "Scan para " + strings.Join(ts, ", ")
			if s.InLoop {
				what = "rows.Scan para " + strings.Join(ts, ", ") + " (a cada linha)"
			}
			if s.Note != "" {
				what = s.Method + ": " + s.Note
			}
			d.add(2, spS(what, sSubtle), spS("  "+loc(s.File, s.Line), sMuted))
		}
		if t := op.Tx; t != nil {
			txt := "transação: " + rolePT(t.Role)
			if t.Var != "" {
				txt += " (" + t.Var + ")"
			}
			if t.FromParam {
				txt += " — transação recebida como parâmetro"
			} else if t.BeginLine > 0 && t.Role != "begin" {
				txt += fmt.Sprintf(", iniciada na linha %d", t.BeginLine)
			}
			if t.Deferred {
				txt += ", adiada"
			}
			d.add(2, spS(txt, sAccent))
		}
		for _, n := range op.Notes {
			d.add(2, spS("! "+n, sWarn))
		}
		for _, p := range plans {
			if p.SQL == strings.TrimSpace(op.SQL.Text) {
				renderPlanSummary(&d, p, 2)
			}
		}
	}
	if len(r.Txs) > 0 {
		d.blank()
		d.add(0, spS("Transações", sBold))
		for _, t := range r.Txs {
			rb := ""
			if t.DeferredRollback {
				rb = " · Rollback adiado"
			}
			d.add(1, sp(fmt.Sprintf("%s iniciada na linha %d em %s: %d operação(ões), commit em %v, rollback em %v%s", t.Var, t.Line, t.Func, len(t.Ops), t.Commits, t.Rollbacks, rb)))
		}
	}
	d.blank()
	for _, n := range r.Notes {
		d.add(0, spS(n, sMuted))
	}
	statusLine(&d, r.Status)
	return d
}

func renderSQLText(d *doc, t sqlops.SQLText) {
	switch {
	case t.Kind == sqlops.TextNone:
		return
	case t.Text != "":
		label := "SQL (" + t.Kind
		if t.Constant != "" {
			label += " " + t.Constant + " em " + loc(t.ConstFile, t.ConstLine)
		}
		label += ", " + t.Statement + "):"
		d.add(2, spS(label, sBold))
		for _, l := range strings.Split(t.Formatted, "\n") {
			d.add(3, spS(l, sAccent))
		}
	case len(t.Variants) > 0:
		d.add(2, spS(fmt.Sprintf("SQL: uma de %d instruções literais (%s)", len(t.Variants), t.Kind), sBold))
		for i, v := range t.Variants {
			d.add(3, spS(fmt.Sprintf("%d) ", i+1), sMuted), spS(oneLineSQL(v), sAccent))
		}
	case t.Template != "":
		d.add(2, spS(inspect.SQLUnresolved+" — "+t.Kind+": ", sWarn), spS("as partes ⟨…⟩ são calculadas em tempo de execução", sMuted))
		for _, l := range strings.Split(t.Formatted, "\n") {
			d.add(3, spS(l, sAccent))
		}
	default:
		d.add(2, spS(inspect.SQLUnresolved, sWarn), spS(" ("+t.Kind+")", sMuted))
	}
	if t.Note != "" && t.Template == "" {
		d.add(3, spS(t.Note, sMuted))
	}
	if t.Multi {
		d.add(3, spS("! o texto contém mais de uma instrução", sWarn))
	}
}

func oneLineSQL(s string) string { return strings.Join(strings.Fields(s), " ") }

// --- PostgreSQL plans ----------------------------------------------------------------

func renderPlanSummary(d *doc, p *pgexplain.Plan, indent int) {
	d.add(indent, spS("Plano de "+p.Target+" (PostgreSQL "+p.Server+"): ", sBold), spS(p.Mode, sMuted))
	renderPlanNode(d, p.Root, indent+1, p.Analyzed)
}

func renderPlan(p *pgexplain.Plan) doc {
	var d doc
	d.add(0, spS("Plano do PostgreSQL", sTitle), spS("  "+p.Target+" · PostgreSQL "+p.Server+" · "+p.At.Format(time.DateTime), sSubtle))
	d.add(0, spS(p.Mode, sAccent))
	for _, l := range strings.Split(sqlops.Format(p.SQL), "\n") {
		d.add(1, spS(l, sSubtle))
	}
	if p.ParamsGiven > 0 {
		d.add(0, spS(fmt.Sprintf("%d valor(es) de parâmetro informado(s); não são gravados.", p.ParamsGiven), sMuted))
	}
	d.blank()
	renderPlanNode(&d, p.Root, 0, p.Analyzed)
	d.blank()
	if len(p.Scans) > 0 {
		d.add(0, spS("Scans: ", sBold), sp(strings.Join(p.Scans, "; ")))
	}
	if len(p.Joins) > 0 {
		d.add(0, spS("Joins: ", sBold), sp(strings.Join(p.Joins, "; ")))
	}
	if len(p.Indexes) > 0 {
		d.add(0, spS("Índices usados: ", sBold), sp(strings.Join(p.Indexes, ", ")))
	}
	if p.PlanningMs != nil {
		d.add(0, spS(fmt.Sprintf("Tempo de planejamento %.3f ms", *p.PlanningMs), sSubtle))
	}
	if p.ExecutionMs != nil {
		d.add(0, spS(fmt.Sprintf("Tempo de execução %.3f ms (observado, uma execução)", *p.ExecutionMs), sSubtle))
	}
	for _, w := range p.Warnings {
		d.add(0, spS(w, sMuted))
	}
	return d
}

func renderPlanNode(d *doc, n *pgexplain.PlanNode, indent int, analyzed bool) {
	if n == nil {
		return
	}
	title := n.NodeType
	if n.JoinType != "" && !strings.Contains(title, n.JoinType) {
		title += " (" + n.JoinType + ")"
	}
	if n.Relation != "" {
		title += " on " + n.Relation
		if n.Alias != "" && n.Alias != n.Relation {
			title += " " + n.Alias
		}
	}
	if n.Index != "" {
		title += " using " + n.Index
	}
	est := fmt.Sprintf("  custo %.2f..%.2f · linhas estimadas %.0f · largura %.0f", n.StartupCost, n.TotalCost, n.PlanRows, n.PlanWidth)
	spans := []tspan{spS("→ ", sMuted), spS(title, sBold), spS(est, sSubtle)}
	if analyzed && n.ActualRows != nil {
		loops := 1.0
		if n.ActualLoops != nil {
			loops = *n.ActualLoops
		}
		t := 0.0
		if n.ActualTotalMs != nil {
			t = *n.ActualTotalMs
		}
		spans = append(spans, spS(fmt.Sprintf("  · linhas reais %.0f × %.0f laços · %.3f ms", *n.ActualRows, loops, t), sAccent))
	}
	d.add(indent, spans...)
	for _, c := range n.Conditions {
		d.add(indent+2, spS(c.Name+": ", sMuted), sp(c.Value))
	}
	for _, k := range n.Keys {
		d.add(indent+2, spS(k.Name+": ", sMuted), sp(k.Value))
	}
	if n.RowsRemovedByFilter != nil {
		d.add(indent+2, spS(fmt.Sprintf("linhas removidas pelo filtro: %.0f", *n.RowsRemovedByFilter), sMuted))
	}
	if n.Buffers != "" {
		d.add(indent+2, spS("buffers: "+n.Buffers, sMuted))
	}
	for _, c := range n.Plans {
		renderPlanNode(d, c, indent+1, analyzed)
	}
}

// --- Performance ---------------------------------------------------------------------

func renderProfile(r *perf.Report, top int, focus string) doc {
	var d doc
	head := fmt.Sprintf("Perfil de %s · %s", kindPT(r.Kind), r.SampleType)
	d.add(0, spS("Performance", sTitle), sp("  "), spS(head, sBold), spS("  "+r.Artifact.Name, sSubtle))
	d.add(0, spS("Perfil amostrado: os valores são estimativas estatísticas ("+r.Meaning+").", sMuted))
	info := fmt.Sprintf("total %s · %d amostras", r.FormatValue(r.Total), r.Samples)
	if r.DurationMs > 0 {
		info += fmt.Sprintf(" · %s de execução perfilada", time.Duration(r.DurationMs*1e6).Round(time.Millisecond))
	}
	if r.Period != "" {
		info += " · " + r.Period
	}
	if len(r.SampleTypes) > 1 {
		info += " · tipos de amostra: " + strings.Join(r.SampleTypes, ", ")
	}
	d.add(0, spS(info, sSubtle))
	if focus != "" {
		fs := r.ForSymbol(focus)
		d.blank()
		if !fs.Found {
			d.add(0, spS("A função selecionada não tem amostras neste perfil.", sMuted))
		} else {
			d.add(0, spS(fmt.Sprintf("Função selecionada: flat %s (%.2f%%) · cum %s (%.2f%%)", r.FormatValue(fs.Flat), fs.FlatPct, r.FormatValue(fs.Cum), fs.CumPct), sAccent))
			for _, l := range fs.Lines {
				d.item(1, "", loc(l.File, l.Line), spS(fmt.Sprintf("linha %-5d flat %-10s cum %s", l.Line, r.FormatValue(l.Flat), r.FormatValue(l.Cum)), sSubtle))
			}
			for _, e := range fs.Callers {
				d.add(1, spS("chamada por ", sMuted), sp(e.From), spS("  "+r.FormatValue(e.Weight), sSubtle))
			}
			for _, e := range fs.Callees {
				d.add(1, spS("chama ", sMuted), sp(e.To), spS("  "+r.FormatValue(e.Weight), sSubtle))
			}
		}
	}
	d.blank()
	d.add(0, spS(fmt.Sprintf("%-12s %7s %12s %7s  %s", "flat", "flat%", "cum", "cum%", "função"), sBold))
	for i, f := range r.Funcs {
		if i >= top {
			d.add(0, spS(fmt.Sprintf("… mais %d função(ões)", len(r.Funcs)-top), sMuted))
			break
		}
		st := sMuted
		if f.Symbol != "" {
			st = sPlain
		}
		ref := ""
		if f.Symbol != "" {
			ref = "sym:" + f.Symbol
		}
		d.item(0, ref, loc(f.File, f.Line), spS(fmt.Sprintf("%-12s %6.2f%% %12s %6.2f%%  ", r.FormatValue(f.Flat), f.FlatPct, r.FormatValue(f.Cum), f.CumPct), sSubtle), spS(f.Name, st))
	}
	for _, n := range r.Notes {
		d.add(0, spS(n, sMuted))
	}
	statusLine(&d, r.Status)
	return d
}

func renderFlame(r *perf.Report, maxDepth int, minPct float64) doc {
	var d doc
	d.add(0, spS("Flame graph (texto): árvore de chamadas a partir da raiz, com a participação acumulada", sBold))
	var walk func(n *perf.FlameNode, depth int)
	walk = func(n *perf.FlameNode, depth int) {
		for _, c := range n.Children {
			pct := 100 * float64(c.Value) / float64(max(r.Total, 1))
			if pct < minPct {
				continue
			}
			bar := strings.Repeat("█", max(int(pct/5), 0))
			st := sMuted
			if c.Symbol != "" {
				st = sPlain
			}
			d.add(depth, spS(fmt.Sprintf("%5.1f%% ", pct), sSubtle), spS(c.Name, st), spS(" "+bar, sAccent))
			if depth < maxDepth {
				walk(c, depth+1)
			}
		}
	}
	walk(r.Flame, 1)
	return d
}

func renderBench(run *perf.BenchRun) doc {
	var d doc
	name := run.Artifact.Name
	d.add(0, spS("Benchmarks", sTitle), spS("  "+name, sSubtle))
	d.add(0, spS("Medido por go test -bench (valores observados nestas execuções; ns/op varia com a carga da máquina).", sMuted))
	var cfg []string
	for _, k := range []string{"goos", "goarch", "pkg", "cpu"} {
		if v := run.Config[k]; v != "" {
			cfg = append(cfg, k+"="+v)
		}
	}
	if len(cfg) > 0 {
		d.add(0, spS(strings.Join(cfg, " · "), sSubtle))
	}
	if run.Artifact.Command != "" {
		d.add(0, spS("$ "+run.Artifact.Command, sMuted))
	}
	d.blank()
	stats := run.Stats()
	d.add(0, spS(fmt.Sprintf("%-40s %-10s %14s %14s %14s %4s", "benchmark", "unidade", "mediana", "mín", "máx", "n"), sBold))
	for _, s := range stats {
		name := s.Name
		if s.Procs > 0 {
			name += fmt.Sprintf("-%d", s.Procs)
		}
		d.add(0, sp(fmt.Sprintf("%-40s %-10s %14.4g %14.4g %14.4g %4d", name, s.Unit, s.Median, s.Min, s.Max, s.N)))
	}
	for _, f := range run.Failures {
		d.add(0, spS(f, sBad))
	}
	return d
}

func renderCompare(c *perf.Comparison) doc {
	var d doc
	d.add(0, spS("Comparação de benchmarks", sTitle), spS("  antigo → novo", sSubtle))
	d.add(0, spS(c.Method, sMuted))
	for _, cd := range c.ConfigDiffs {
		d.add(0, spS("! condições diferentes: "+cd, sWarn))
	}
	if c.Benchstat != "" {
		d.blank()
		d.add(0, spS("saída do benchstat:", sBold))
		for _, l := range strings.Split(strings.TrimRight(c.Benchstat, "\n"), "\n") {
			d.add(1, sp(l))
		}
		d.blank()
	}
	d.add(0, spS(fmt.Sprintf("%-40s %-10s %14s %14s %9s %8s  %s", "benchmark", "unidade", "mediana antiga", "mediana nova", "Δ", "p", "nota"), sBold))
	for _, r := range c.Rows {
		old, cur := "-", "-"
		if r.Old != nil {
			old = fmt.Sprintf("%.4g", r.Old.Median)
		}
		if r.New != nil {
			cur = fmt.Sprintf("%.4g", r.New.Median)
		}
		delta, p := "", ""
		if r.Old != nil && r.New != nil {
			delta = fmt.Sprintf("%+.2f%%", r.DeltaPct)
			if r.DeltaUndefined {
				delta = "de 0"
			}
		}
		if r.P >= 0 {
			p = fmt.Sprintf("%.3f", r.P)
		}
		st := sPlain
		if r.Significant {
			st = sGood
			if r.DeltaPct > 0 {
				st = sBad
			}
		}
		d.add(0, spS(fmt.Sprintf("%-40s %-10s %14s %14s %9s %8s  ", r.Name, r.Unit, old, cur, delta, p), st), spS(r.Note, sMuted))
	}
	return d
}

func renderEscape(r *perf.EscapeReport, focus string, max int) doc {
	var d doc
	d.add(0, spS("Diagnósticos do compilador", sTitle), spS("  go build -gcflags=-m=2 · "+strings.Join(r.Packages, ", "), sSubtle))
	d.add(0, spS("Análise de escape e decisões de inlining, literalmente como o compilador imprimiu neste build.", sMuted))
	counts := r.Counts()
	var cs []string
	for _, k := range []string{perf.DiagEscapes, perf.DiagMoved, perf.DiagNoEscape, perf.DiagLeaking, perf.DiagCanInline, perf.DiagCannot, perf.DiagInlineCall} {
		if counts[k] > 0 {
			cs = append(cs, fmt.Sprintf("%s %d", k, counts[k]))
		}
	}
	d.add(0, spS(strings.Join(cs, " · "), sSubtle))
	diags := r.Diags
	if focus != "" {
		diags = r.ForSymbol(focus)
		if len(diags) == 0 {
			d.add(0, spS("Nenhum diagnóstico localizado na função selecionada.", sMuted))
		}
	}
	d.blank()
	for i, dg := range diags {
		if max > 0 && i >= max {
			d.add(0, spS(fmt.Sprintf("… mais %d", len(diags)-max), sMuted))
			break
		}
		st := sPlain
		switch dg.Kind {
		case perf.DiagEscapes, perf.DiagMoved:
			st = sWarn
		case perf.DiagNoEscape, perf.DiagCanInline, perf.DiagInlineCall:
			st = sSubtle
		}
		fn := ""
		if dg.Func != "" {
			fn = "  em " + dg.Func
		}
		d.item(0, "", loc(dg.File, dg.Line), spS(dg.Message, st), spS(fn, sMuted))
		for _, f := range dg.Flow {
			d.add(2, spS(f, sMuted))
		}
	}
	for _, e := range r.Errors {
		d.add(0, spS(e, sBad))
	}
	return d
}

// --- Execution Trace ---------------------------------------------------------------

func fmtDur(ns int64) string {
	d := time.Duration(ns)
	switch {
	case d >= time.Second:
		return d.Round(time.Millisecond).String()
	case d >= time.Millisecond:
		return d.Round(10 * time.Microsecond).String()
	case d >= time.Microsecond:
		return d.Round(100 * time.Nanosecond).String()
	}
	return d.String()
}

// timelineRow draws a goroutine's states between from and to over width
// cells: █ running, ▒ runnable, ░ waiting, ▓ syscall.
func timelineRow(g *xtrace.Goroutine, from, to int64, width int) string {
	cells := make([]rune, width)
	weight := make([]int64, width)
	for i := range cells {
		cells[i] = ' '
	}
	scale := float64(width) / float64(max(to-from, 1))
	for _, s := range g.Segments {
		if s.End < from || s.Start > to {
			continue
		}
		a := int(float64(max(s.Start, from)-from) * scale)
		b := int(float64(min(s.End, to)-from) * scale)
		ch := map[string]rune{xtrace.Running: '█', xtrace.Runnable: '▒', xtrace.Waiting: '░', xtrace.Syscall: '▓'}[s.State]
		for c := a; c <= b && c < width; c++ {
			if c < 0 {
				continue
			}
			w := s.End - s.Start
			if s.State == xtrace.Running {
				w *= 4 // running wins over waiting in a shared cell
			}
			if w >= weight[c] {
				weight[c], cells[c] = w, ch
			}
		}
	}
	return string(cells)
}

func gcRow(t *xtrace.Timeline, from, to int64, width int) string {
	cells := []rune(strings.Repeat(" ", width))
	scale := float64(width) / float64(max(to-from, 1))
	for _, s := range t.GC {
		if s.End < from || s.Start > to {
			continue
		}
		a := int(float64(max(s.Start, from)-from) * scale)
		b := int(float64(min(s.End, to)-from) * scale)
		ch := '▄'
		if strings.HasPrefix(s.Name, "stop-the-world") {
			ch = '█'
		}
		for c := max(a, 0); c <= b && c < width; c++ {
			cells[c] = ch
		}
	}
	return string(cells)
}

func renderTrace(t *xtrace.Timeline, focus string, width int, userOnly bool, from, to int64) doc {
	if to <= from || to > t.DurationNs {
		from, to = 0, t.DurationNs
	}
	width = max(width, 60)
	var d doc
	d.add(0, spS("Execution Trace", sTitle), spS("  "+t.Artifact.Name+" · eventos observados (runtime/trace)", sSubtle))
	if t.Artifact.Command != "" {
		d.add(0, spS("$ "+t.Artifact.Command, sMuted))
	}
	s := t.Stats
	d.add(0, spS(fmt.Sprintf("duração %s · %d goroutines (%d fora do runtime) · %d Ps · %d ciclos de GC (%s) · %d tarefas · %d regiões · %d logs · %d eventos",
		fmtDur(t.DurationNs), s.Goroutines, s.UserGoroutines, s.Procs, s.GCCycles, fmtDur(s.GCNs), len(t.Tasks), len(t.Regions), len(t.Logs), s.Events), sSubtle))
	width = max(width, 20)
	d.blank()
	d.add(0, spS(fmt.Sprintf("%-28s %s", "", fmtDur(from)), sMuted), spS(fmt.Sprintf("%*s", max(width-29-len(fmtDur(from)), 1), fmtDur(to)), sMuted))
	for _, g := range t.Goroutines {
		if userOnly && g.System {
			continue
		}
		name := fmt.Sprintf("G%-4d %s", g.ID, shortFunc(g.Name))
		st := sSubtle
		if g.Symbol != "" {
			st = sPlain
		}
		d.item(0, fmt.Sprintf("g:%d", g.ID), "", spS(fmt.Sprintf("%-28.28s ", name), st), spS(timelineRow(g, from, to, width-29), sAccent))
	}
	d.add(0, spS(fmt.Sprintf("%-28s ", "GC / STW"), sWarn), spS(gcRow(t, from, to, width-29), sWarn))
	d.add(0, spS("█ executando  ▒ pronta  ░ esperando  ▓ syscall   GC: ▄ fase de GC  █ stop-the-world", sMuted))
	if focus != "" {
		st := t.ForSymbol(focus)
		d.blank()
		d.add(0, spS("Função selecionada neste trace", sBold))
		if !st.Found {
			d.add(1, spS("Nenhum evento deste trace é atribuído a ela (nenhuma goroutine iniciada nela, nenhuma região medindo-a, nenhum bloqueio com ela na pilha).", sMuted))
		}
		if len(st.Goroutines) > 0 {
			d.add(1, sp(fmt.Sprintf("goroutines iniciadas nela: %v", st.Goroutines)))
		}
		if st.SpanCount > 0 {
			d.add(1, sp(fmt.Sprintf("spans medidos (regiões): %d · total %s · mín %s · máx %s · média %s", st.SpanCount, fmtDur(st.SpanTotal), fmtDur(st.SpanMin), fmtDur(st.SpanMax), fmtDur(st.SpanTotal/int64(st.SpanCount)))))
		}
		for i, b := range st.Blocks {
			if i >= 8 {
				d.add(1, spS(fmt.Sprintf("… mais %d bloqueios", len(st.Blocks)-8), sMuted))
				break
			}
			d.add(1, spS(fmt.Sprintf("G%d bloqueada (%s) por %s em %s", b.G, b.Detail, fmtDur(b.Dur), fmtDur(b.T)), sWarn), spS("  "+stackTop(t, b.Stack, focus), sMuted))
		}
	}
	if blocks := t.LongestBlocks(8); len(blocks) > 0 {
		d.blank()
		d.add(0, spS("Esperas mais longas observadas (goroutines fora do runtime)", sBold))
		for i, b := range blocks {
			d.item(1, fmt.Sprintf("e:%d", i), "", spS(fmt.Sprintf("%-10s", fmtDur(b.Dur)), sWarn), sp(fmt.Sprintf("G%d %s", b.G, b.Detail)), spS("  em "+fmtDur(b.T)+"  "+stackTop(t, b.Stack, ""), sMuted))
		}
	}
	if len(t.Tasks) > 0 {
		d.blank()
		d.add(0, spS("Tarefas", sBold))
		for _, tk := range t.Tasks {
			dur := "não terminou"
			if tk.End >= 0 {
				dur = fmtDur(tk.End - tk.Start)
			}
			d.add(1, sp(fmt.Sprintf("%s (#%d): %s, goroutines %v", tk.Type, tk.ID, dur, tk.Goroutines)))
		}
	}
	regions := map[string][]int64{}
	var order []string
	for _, r := range t.Regions {
		if r.End < 0 {
			continue
		}
		if _, ok := regions[r.Type]; !ok {
			order = append(order, r.Type)
		}
		regions[r.Type] = append(regions[r.Type], r.End-r.Start)
	}
	if len(order) > 0 {
		d.blank()
		d.add(0, spS("Regiões (durações observadas)", sBold))
		sort.Strings(order)
		for _, k := range order {
			ds := regions[k]
			var total, mx int64
			for _, x := range ds {
				total += x
				mx = max(mx, x)
			}
			label := k
			if strings.HasPrefix(k, xtrace.RegionPrefix) {
				label += "  (instrumentação do GitView)"
			}
			d.add(1, sp(fmt.Sprintf("%-60s ×%-5d total %-10s média %-10s máx %s", label, len(ds), fmtDur(total), fmtDur(total/int64(len(ds))), fmtDur(mx))))
		}
	}
	if len(t.Logs) > 0 {
		d.blank()
		d.add(0, spS("Logs", sBold))
		for i, l := range t.Logs {
			if i >= 20 {
				break
			}
			d.add(1, sp(fmt.Sprintf("%s G%d [%s] %s", fmtDur(l.T), l.G, l.Category, l.Message)))
		}
	}
	d.blank()
	for _, n := range t.Notes {
		d.add(0, spS(n, sMuted))
	}
	statusLine(&d, t.Status)
	return d
}

func shortFunc(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// stackTop names the most relevant frame: the focused symbol if present,
// otherwise the first frame outside the runtime.
func stackTop(t *xtrace.Timeline, i int, focus string) string {
	if i < 0 || i >= len(t.Stacks) {
		return ""
	}
	frames := t.Stacks[i]
	for _, f := range frames {
		if focus != "" && f.Symbol == focus {
			return shortFunc(f.Func) + " " + loc(f.File, f.Line)
		}
	}
	for _, f := range frames {
		if f.Symbol != "" {
			return shortFunc(f.Func) + " " + loc(f.File, f.Line)
		}
	}
	for _, f := range frames {
		if !strings.HasPrefix(f.Func, "runtime.") && !strings.HasPrefix(f.Func, "internal/") && !strings.HasPrefix(f.Func, "sync.") && !strings.HasPrefix(f.Func, "time.") {
			return shortFunc(f.Func) + " " + loc(f.File, f.Line)
		}
	}
	return shortFunc(frames[0].Func)
}

func statusPT(st evidence.Status) string {
	switch st {
	case evidence.Partial:
		return "parcial"
	case evidence.Inconclusive:
		return "inconclusivo"
	case evidence.Unavailable:
		return "indisponível"
	}
	return string(st)
}

func terminalPT(t string) string {
	switch t {
	case branch.TermImplicit:
		return "retorno implícito"
	case branch.TermBlocked:
		return "bloqueia para sempre"
	}
	return t
}

func selKindPT(k string) string {
	return map[string]string{"variable": "a variável", "param": "o parâmetro", "return": "o", "expr": "a expressão", "value": "o valor"}[k]
}

func dirPT(d dataflow.Direction) string {
	return map[dataflow.Direction]string{dataflow.Origin: "origem", dataflow.Destination: "destino", dataflow.Both: "ambas"}[d]
}

func rolePT(r string) string {
	return map[string]string{"begin": "início", "in transaction": "dentro da transação", "commit": "commit", "rollback": "rollback"}[r]
}

func kindPT(k string) string {
	return map[string]string{"cpu": "CPU", "heap": "memória", "contention": "contenção (bloqueio ou mutex)", "goroutine": "goroutines", "other": "outro tipo"}[k]
}

// extraStmts returns the statements of a block not shown in its summary.
func extraStmts(b *branch.Block) []branch.Stmt {
	n := len(b.Stmts)
	switch {
	case b.Decision != "" || n == 0:
		return b.Stmts
	case b.Terminal == branch.TermImplicit:
		if n > 2 {
			return b.Stmts[1 : n-1]
		}
		return nil
	case b.Terminal != "":
		return b.Stmts[:n-1]
	}
	return b.Stmts[1:]
}
