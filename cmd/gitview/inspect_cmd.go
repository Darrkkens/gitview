package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/export"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect"
	"github.com/athos/gitview/internal/inspect/branch"
	"github.com/athos/gitview/internal/inspect/dataflow"
	"github.com/athos/gitview/internal/inspect/perf"
	"github.com/athos/gitview/internal/inspect/pgexplain"
	"github.com/athos/gitview/internal/inspect/run"
	"github.com/athos/gitview/internal/inspect/sqlops"
	"github.com/athos/gitview/internal/inspect/xtrace"
)

const inspectUsage = `uso: gitview inspect [subcomando] [opções] [diretório]

Go Deep Inspector: estuda uma função Go com evidências verificáveis.

  gitview inspect --function GetClientByID                 resumo de todas as visões
  gitview inspect dataflow --function GetClient --variable clientID
  gitview inspect branches --function GetClientByID --paths
  gitview inspect sql --function FindByID
  gitview inspect sql --function FindByID --explain --dsn postgres://localhost/dev
  gitview inspect performance --cpu cpu.prof --function Busy
  gitview inspect performance --memory mem.prof
  gitview inspect performance --bench . --package ./perf       (executa benchmarks)
  gitview inspect performance --escape ./perf                  (compila com -gcflags=-m=2)
  gitview inspect performance --compare old.txt,new.txt
  gitview inspect trace --import trace.out
  gitview inspect trace --test TestPipeline --package ./traced --instrument Square
  gitview inspect report --function GetClientByID -o inspect.html

Subcomandos: dataflow, branches, sql, performance (perf), trace, report.
Use "gitview inspect <subcomando> -h" para as opções de cada um.

Resultados estáticos descrevem caminhos possíveis; traces e benchmarks são
observações de uma execução; perfis e planos de PostgreSQL são estimativas.
Nada é executado sem confirmação explícita (--yes em modo não interativo).
`

// Exit codes.
const (
	exitOK        = 0
	exitErr       = 1
	exitUsage     = 2
	exitAmbiguous = 3
	exitConsent   = 4
)

type inspectEnv struct {
	stdout, stderr io.Writer
	stdin          io.Reader
	ctx            context.Context
	root           string
	sess           *inspect.Session
	nonInter       bool
	yes            bool
	jsonOut        bool
}

func runInspect(args []string, stdout, stderr io.Writer) int {
	return runInspectIO(args, stdout, stderr, os.Stdin)
}

func runInspectIO(args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	env := &inspectEnv{stdout: stdout, stderr: stderr, stdin: stdin, ctx: ctx}
	switch sub {
	case "":
		return env.summary(args)
	case "dataflow", "data-flow", "df":
		return env.dataflow(args)
	case "branches", "branch", "cfg":
		return env.branches(args)
	case "sql":
		return env.sql(args)
	case "performance", "perf":
		return env.performance(args)
	case "trace", "execution-trace":
		return env.trace(args)
	case "report":
		return env.report(args)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, inspectUsage)
		return exitOK
	}
	fmt.Fprintf(stderr, "gitview inspect: subcomando desconhecido %q\n\n%s", sub, inspectUsage)
	return exitUsage
}

func (e *inspectEnv) flags(name, usage string) *flag.FlagSet {
	fs := flag.NewFlagSet("inspect "+name, flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	fs.BoolVar(&e.nonInter, "non-interactive", false, "nunca pergunta nem abre seletores; homônimos geram erro")
	fs.BoolVar(&e.yes, "yes", false, "confirma execuções (testes, benchmarks, builds, EXPLAIN) sem perguntar")
	fs.BoolVar(&e.jsonOut, "json", false, "saída em JSON")
	fs.Usage = func() { fmt.Fprint(e.stderr, usage); fs.PrintDefaults() }
	return fs
}

func (e *inspectEnv) fail(err error) int {
	fmt.Fprintln(e.stderr, "gitview inspect:", err)
	var amb *flow.AmbiguityError
	var damb *dataflow.AmbiguousError
	switch {
	case errors.As(err, &amb), errors.As(err, &damb):
		return exitAmbiguous
	case errors.Is(err, errConsent):
		return exitConsent
	case errors.Is(err, context.Canceled):
		return 130
	}
	return exitErr
}

// load analyses the module(s) of dir.
func (e *inspectEnv) load(dir string) error {
	root, progs, err := loadPrograms(e.ctx, dir, e.stderr)
	if err != nil {
		return err
	}
	e.root = root
	e.sess = inspect.NewSession(root, progs)
	return nil
}

// symbol resolves --function with the same rules as Code Flow: homonyms
// open a selector in a terminal and are an error otherwise.
func (e *inspectEnv) symbol(q string) (*inspect.Resolved, error) {
	if q == "" {
		return nil, errors.New("informe --function (nome, nome qualificado, ID ou arquivo:linha)")
	}
	progs := e.sess.Programs()
	p, sym, err := pickSymbol(progs, q, !e.nonInter && isTerminal(), e.stderr)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, context.Canceled
	}
	fn := p.FuncByID(sym.ID)
	if fn == nil {
		return nil, fmt.Errorf("%s: %q", inspect.NoSymbol, q)
	}
	return &inspect.Resolved{Prog: p, Fn: fn, Ref: p.Ref(fn)}, nil
}

var errConsent = errors.New("execução não confirmada")

// confirm shows what will run and asks for consent.
func (e *inspectEnv) confirm(what string, lines ...string) error {
	fmt.Fprintf(e.stderr, "\n%s\n", what)
	for _, l := range lines {
		fmt.Fprintf(e.stderr, "  %s\n", l)
	}
	if e.yes {
		fmt.Fprintln(e.stderr, "  (confirmado por --yes)")
		return nil
	}
	if e.nonInter || !isTerminal() {
		fmt.Fprintln(e.stderr, "Nada foi executado: confirme com --yes.")
		return errConsent
	}
	fmt.Fprint(e.stderr, "Executar? [s/N] ")
	ans, _ := bufio.NewReader(e.stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(ans)) {
	case "s", "sim", "y", "yes":
		return nil
	}
	fmt.Fprintln(e.stderr, "Cancelado: nada foi executado.")
	return errConsent
}

func (e *inspectEnv) printJSON(v any) int {
	enc := json.NewEncoder(e.stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return e.fail(err)
	}
	return exitOK
}

func dirArg(fs *flag.FlagSet) string {
	if fs.NArg() > 0 {
		return fs.Arg(0)
	}
	return "."
}

// pkgPattern returns ./rel/dir for the package of a symbol, relative to its module.
func pkgPattern(r *inspect.Resolved) string {
	abs := filepath.Join(r.Prog.RepoRoot, r.Ref.File)
	rel, err := filepath.Rel(r.Prog.Dir, filepath.Dir(abs))
	if err != nil || rel == "." {
		return "."
	}
	return "./" + filepath.ToSlash(rel)
}

// --- summary --------------------------------------------------------------------

func (e *inspectEnv) summary(args []string) int {
	var fn, cpu, mem, tr, cover, bench string
	fs := e.flags("", inspectUsage)
	fs.StringVar(&fn, "function", "", "função: nome, nome qualificado, ID ou arquivo:linha")
	fs.StringVar(&fn, "f", "", "atalho para --function")
	fs.StringVar(&cpu, "cpu", "", "perfil de CPU (pprof) a relacionar")
	fs.StringVar(&mem, "memory", "", "perfil de memória (pprof) a relacionar")
	fs.StringVar(&tr, "trace", "", "trace de execução (go test -trace) a relacionar")
	fs.StringVar(&cover, "cover", "", "perfil de cobertura (go test -coverprofile)")
	fs.StringVar(&bench, "bench-file", "", "saída de go test -bench a relacionar")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fn == "" {
		fs.Usage()
		return exitUsage
	}
	if err := e.load(dirArg(fs)); err != nil {
		return e.fail(err)
	}
	if err := e.loadArtifacts(cpu, mem, tr, cover, bench); err != nil {
		return e.fail(err)
	}
	r, err := e.symbol(fn)
	if err != nil {
		return e.fail(err)
	}
	b := e.sess.Bundle(r, inspect.BundleOptions{})
	if e.jsonOut {
		return e.printJSON(b)
	}
	fmt.Fprint(e.stdout, renderSummary(r, b, e.sess).plain())
	return exitOK
}

func (e *inspectEnv) loadArtifacts(cpu, mem, tr, cover, bench string) error {
	progs := e.sess.Programs()
	if cpu != "" {
		r, err := perf.Load(cpu, progs, perf.Options{})
		if err != nil {
			return err
		}
		e.sess.CPU = r
	}
	if mem != "" {
		r, err := perf.Load(mem, progs, perf.Options{SampleType: "alloc_space"})
		if err != nil {
			r, err = perf.Load(mem, progs, perf.Options{})
		}
		if err != nil {
			return err
		}
		e.sess.Mem = r
	}
	if tr != "" {
		t, err := xtrace.Load(tr, progs, xtrace.Limits{})
		if err != nil {
			return err
		}
		e.sess.Trace = t
	}
	if cover != "" {
		c, err := branch.LoadProfile(cover)
		if err != nil {
			return err
		}
		e.sess.Coverage = c
	}
	if bench != "" {
		b, err := perf.LoadBench(bench)
		if err != nil {
			return err
		}
		e.sess.AddBench(b)
	}
	return nil
}

// renderSummary is the overview of every view for one symbol.
func renderSummary(r *inspect.Resolved, b *inspect.Bundle, sess *inspect.Session) doc {
	var d doc
	ref := b.Symbol
	d.add(0, tspan{"Go Deep Inspector", sTitle}, sp("  "), spS(ref.Qualified, sBold))
	d.add(1, spS("ID         ", sMuted), sp(ref.ID))
	d.add(1, spS("Assinatura ", sMuted), sp(ref.Signature))
	d.add(1, spS("Local      ", sMuted), sp(fmt.Sprintf("%s:%d-%d", ref.File, ref.Line, ref.EndLine)))
	d.add(1, spS("Módulo     ", sMuted), sp(ref.Module+" · versão do código "+ref.Version))

	d.blank()
	d.add(0, spS("[Code Flow]", sAccent), spS("  grafo de chamadas estático", sMuted))
	if g, err := r.Prog.Query(r.Fn, flow.Both, flow.Options{Depth: 1, External: true}); err == nil {
		var callers, callees []string
		for _, e := range g.In(ref.ID) {
			if n := g.Node(e.From); n != nil {
				callers = append(callers, n.Name+confMark[e.Confidence])
			}
		}
		for _, e := range g.Out(ref.ID) {
			if n := g.Node(e.To); n != nil {
				callees = append(callees, n.Name+confMark[e.Confidence])
			}
		}
		d.add(1, spS("chamadores: ", sMuted), sp(orNone(uniq(callers))))
		d.add(1, spS("chamadas:   ", sMuted), sp(orNone(uniq(callees))))
	}

	d.blank()
	d.add(0, spS("[Data Flow]", sAccent), spS("  def-use estático (SSA)", sMuted))
	var vars []string
	for _, v := range b.Variables {
		vars = append(vars, fmt.Sprintf("%s %s (%s, linha %d)", v.Kind, v.Name, v.Type, v.Line))
	}
	d.add(1, spS("variáveis: ", sMuted), sp(orNone(vars)))
	if len(b.Variables) > 0 {
		d.add(1, spS(fmt.Sprintf("gitview inspect dataflow --function '%s' --variable %s", ref.ID, b.Variables[0].Name), sSubtle))
	}

	d.blank()
	d.add(0, spS("[Execution Trace]", sAccent), spS("  eventos observados", sMuted))
	if !b.Trace.Loaded {
		d.add(1, spS(inspect.NoTrace, sMuted))
	} else if st := b.Trace.Symbol; st != nil {
		d.add(1, sp(fmt.Sprintf("%s: %d goroutine(s) iniciada(s) aqui, %d span(s) medido(s), %d bloqueio(s)", b.Trace.Trace, len(st.Goroutines), st.SpanCount, len(st.Blocks))))
		if b.Trace.Message != "" {
			d.add(1, spS(b.Trace.Message, sMuted))
		}
	}

	d.blank()
	d.add(0, spS("[Branch Explorer]", sAccent), spS("  grafo de fluxo de controle (estrutural)", sMuted))
	if g := b.Branches; g != nil {
		m := g.Metrics
		d.add(1, sp(fmt.Sprintf("%d blocos · %d decisões · complexidade ciclomática %d · %d retornos · %d panic/exit · %d laços · %d caminhos", m.Blocks, m.DecisionPoints, m.Cyclomatic, m.Returns, m.Panics, m.Loops, m.Paths)))
		for _, row := range g.Outline() {
			bl := g.Block(row.Block)
			if row.Ref || row.Loop || row.Join {
				continue
			}
			label := ""
			if row.Label != "" {
				label = row.Label + " → "
			}
			d.add(row.Depth+1, spS(label, sMuted), sp(bl.Summary()), spS(coverMark(bl.Coverage).text, sMuted))
		}
	} else if msg := b.Errors["branches"]; msg != "" {
		d.add(1, spS(msg, sWarn))
	}

	d.blank()
	d.add(0, spS("[Performance]", sAccent), spS("  perfis são estimativas; benchmarks são medições", sMuted))
	pv := b.Perf
	if pv.Message != "" {
		d.add(1, spS(pv.Message, sMuted))
	}
	if pv.CPU != nil {
		if pv.CPU.Found {
			d.add(1, sp(fmt.Sprintf("CPU (%s): flat %.2f%% · cum %.2f%%", pv.CPUProfile, pv.CPU.FlatPct, pv.CPU.CumPct)))
		} else {
			d.add(1, spS("CPU ("+pv.CPUProfile+"): sem amostras nesta função", sMuted))
		}
	}
	if pv.Mem != nil {
		if pv.Mem.Found {
			d.add(1, sp(fmt.Sprintf("Memória (%s, %s): flat %.2f%% · cum %.2f%%", pv.MemProfile, pv.MemType, pv.Mem.FlatPct, pv.Mem.CumPct)))
		} else {
			d.add(1, spS("Memória ("+pv.MemProfile+"): sem amostras nesta função", sMuted))
		}
	}
	for _, dg := range pv.Escape {
		d.add(1, spS(dg.Message, sSubtle), spS("  "+loc(dg.File, dg.Line), sMuted))
	}
	for _, s := range pv.Benchmarks {
		d.add(1, sp(fmt.Sprintf("%s %s mediana %.4g (n=%d)", s.Name, s.Unit, s.Median, s.N)))
	}

	d.blank()
	d.add(0, spS("[SQL Explorer]", sAccent), spS("  descoberta estática", sMuted))
	if len(b.SQL.Ops) == 0 {
		d.add(1, spS(inspect.NoSQL, sMuted))
	}
	for _, op := range b.SQL.Ops {
		sql := oneLineSQL(op.SQL.Text)
		switch {
		case sql != "":
		case op.SQL.Template != "":
			sql = inspect.SQLUnresolved + ": " + oneLineSQL(op.SQL.Template)
		case op.SQL.Kind == sqlops.TextNone:
			sql = ""
		default:
			sql = inspect.SQLUnresolved
		}
		via := ""
		if len(op.Via) > 0 {
			via = " via " + strings.Join(op.Via, " → ")
		}
		d.add(1, spS(op.Lib+" "+op.Op, sGood), spS(" "+loc(op.File, op.Line)+via, sMuted), sp("  "+sql))
	}
	return d
}

func orNone(xs []string) string {
	if len(xs) == 0 {
		return "(nenhum)"
	}
	return strings.Join(xs, ", ")
}

// --- dataflow --------------------------------------------------------------------

const dataflowUsage = `uso: gitview inspect dataflow --function F (--variable V [--line N] | --return K | --at arquivo:linha:coluna) [opções] [diretório]

Rastreia de onde um valor vem (origin) e para onde pode ir (destination),
com cadeias def-use do SSA, entre funções até a profundidade escolhida.

`

func (e *inspectEnv) dataflow(args []string) int {
	var fn, variable, at, direction string
	var line, ret, depth, maxNodes int
	var hide bool
	fs := e.flags("dataflow", dataflowUsage)
	fs.StringVar(&fn, "function", "", "função analisada")
	fs.StringVar(&fn, "f", "", "atalho para --function")
	fs.StringVar(&variable, "variable", "", "variável ou parâmetro")
	fs.StringVar(&variable, "v", "", "atalho para --variable")
	fs.IntVar(&line, "line", 0, "linha: escolhe entre variáveis homônimas ou restringe às referências da linha")
	fs.IntVar(&ret, "return", -1, "rastreia o k-ésimo resultado (0 = primeiro)")
	fs.StringVar(&at, "at", "", "expressão em arquivo:linha:coluna")
	fs.StringVar(&direction, "direction", "both", "origin, destination ou both")
	fs.IntVar(&depth, "depth", 3, "fronteiras de função atravessadas (1-8)")
	fs.IntVar(&maxNodes, "max-nodes", 300, "limite de nós")
	fs.BoolVar(&hide, "hide-intermediate", false, "oculta valores intermediários do SSA (conversões implícitas, temporários)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	dir := dataflow.Direction(direction)
	if dir != dataflow.Origin && dir != dataflow.Destination && dir != dataflow.Both {
		fmt.Fprintln(e.stderr, "gitview inspect dataflow: --direction deve ser origin, destination ou both")
		return exitUsage
	}
	if depth < 1 || depth > 8 {
		fmt.Fprintln(e.stderr, "gitview inspect dataflow: --depth deve estar entre 1 e 8")
		return exitUsage
	}
	if err := e.load(dirArg(fs)); err != nil {
		return e.fail(err)
	}
	r, err := e.symbol(fn)
	if err != nil {
		return e.fail(err)
	}
	q := dataflow.Query{Variable: variable, Line: line, Return: ret}
	if at != "" {
		parts := strings.Split(at, ":")
		if len(parts) < 3 {
			fmt.Fprintln(e.stderr, "gitview inspect dataflow: --at espera arquivo:linha:coluna")
			return exitUsage
		}
		q.File = strings.Join(parts[:len(parts)-2], ":")
		q.Line, _ = strconv.Atoi(parts[len(parts)-2])
		q.Col, _ = strconv.Atoi(parts[len(parts)-1])
		q.Variable = ""
	}
	if q.Variable == "" && q.Return < 0 && q.File == "" {
		fmt.Fprintf(e.stderr, "Escolha o que rastrear em %s com --variable, --return ou --at:\n", r.Ref.Qualified)
		for _, v := range dataflow.Variables(r.Prog, r.Fn) {
			fmt.Fprintf(e.stderr, "  %-7s %-16s %-24s linha %d\n", v.Kind, v.Name, v.Type, v.Line)
		}
		return exitUsage
	}
	res, err := e.sess.DataFlow(r, q, dataflow.Options{Direction: dir, MaxDepth: depth, MaxNodes: maxNodes})
	if err != nil {
		return e.fail(err)
	}
	if e.jsonOut {
		if hide {
			res = res.HideIntermediate()
		}
		return e.printJSON(res)
	}
	fmt.Fprint(e.stdout, renderDataFlow(res, hide).plain())
	return exitOK
}

// --- branches ---------------------------------------------------------------------

const branchesUsage = `uso: gitview inspect branches --function F [opções] [diretório]

Mostra o grafo de fluxo de controle (CFG) da função: decisões, laços,
retornos, panics, defers, blocos inalcançáveis e caminhos até as saídas.

`

func (e *inspectEnv) branches(args []string) int {
	var fn, cover string
	var paths, runCover, dot bool
	var timeout time.Duration
	fs := e.flags("branches", branchesUsage)
	fs.StringVar(&fn, "function", "", "função analisada")
	fs.StringVar(&fn, "f", "", "atalho para --function")
	fs.BoolVar(&paths, "paths", false, "lista os caminhos até cada saída")
	fs.StringVar(&cover, "cover", "", "sobrepõe um perfil de cobertura (go test -coverprofile)")
	fs.BoolVar(&runCover, "run-coverage", false, "executa os testes do pacote com -coverprofile (pede confirmação)")
	fs.BoolVar(&dot, "dot", false, "imprime o grafo no formato Graphviz")
	fs.DurationVar(&timeout, "timeout", 5*time.Minute, "tempo limite da execução dos testes")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if err := e.load(dirArg(fs)); err != nil {
		return e.fail(err)
	}
	r, err := e.symbol(fn)
	if err != nil {
		return e.fail(err)
	}
	if cover != "" {
		if e.sess.Coverage, err = branch.LoadProfile(cover); err != nil {
			return e.fail(err)
		}
	}
	if runCover {
		out := run.ArtifactPath(e.root, "coverage")
		file := filepath.Join(out, "cover.out")
		args := branch.CoverArgs(file, pkgPattern(r))
		spec := run.Spec{Args: args, Dir: r.Prog.Dir, Timeout: timeout, Live: e.stderr}
		rel, _ := filepath.Rel(e.root, r.Prog.Dir)
		if err := e.confirm("Os testes do pacote serão executados (código do repositório, possíveis efeitos externos):",
			"$ "+spec.CommandLine(), "em "+rel); err != nil {
			return e.fail(err)
		}
		if err := os.MkdirAll(out, 0o755); err != nil {
			return e.fail(err)
		}
		res := run.Run(e.ctx, spec)
		if res.Err != nil {
			return e.fail(res.Err)
		}
		if res.ExitCode != 0 {
			fmt.Fprintf(e.stderr, "go test terminou com código %d; a cobertura reflete o que foi executado.\n", res.ExitCode)
		}
		if e.sess.Coverage, err = branch.LoadProfile(file); err != nil {
			return e.fail(err)
		}
		fmt.Fprintln(e.stderr, "cobertura gravada em", file)
	}
	g, err := e.sess.Branches(r)
	if err != nil {
		return e.fail(err)
	}
	switch {
	case dot:
		fmt.Fprint(e.stdout, g.Dot())
	case e.jsonOut:
		return e.printJSON(g)
	default:
		fmt.Fprint(e.stdout, renderBranches(g, paths).plain())
	}
	return exitOK
}

// --- sql --------------------------------------------------------------------------

const sqlUsage = `uso: gitview inspect sql (--function F | --all) [opções] [diretório]

Encontra as operações de banco (pgx, pgxpool, database/sql, sqlx): SQL,
parâmetros e sua origem (Data Flow), Scan, transações. Com --explain,
consulta o plano no PostgreSQL configurado explicitamente.

O plano só é pedido a servidores locais (loopback ou socket Unix), salvo
--allow-remote. EXPLAIN roda numa transação somente leitura desfeita no fim.
EXPLAIN ANALYZE executa a instrução: exige --analyze, e instruções que
alteram dados exigem também --allow-write (sempre com ROLLBACK).

`

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func (e *inspectEnv) sql(args []string) int {
	var fn, dsn string
	var all, reachable, explain, analyze, allowWrite, allowRemote, buffers bool
	var depth, opIdx int
	var params multiFlag
	var timeout time.Duration
	fs := e.flags("sql", sqlUsage)
	fs.StringVar(&fn, "function", "", "função analisada")
	fs.StringVar(&fn, "f", "", "atalho para --function")
	fs.BoolVar(&all, "all", false, "todas as operações do módulo")
	fs.BoolVar(&reachable, "reachable", false, "inclui operações das funções chamadas")
	fs.IntVar(&depth, "depth", 4, "profundidade de chamadas com --reachable")
	fs.BoolVar(&explain, "explain", false, "pede o plano ao PostgreSQL (EXPLAIN, não executa a instrução)")
	fs.IntVar(&opIdx, "op", 0, "operação a explicar (número da lista)")
	fs.StringVar(&dsn, "dsn", "", "conexão PostgreSQL (ou "+pgexplain.EnvDSN+"); nunca é gravada em relatórios")
	fs.BoolVar(&analyze, "analyze", false, "EXPLAIN ANALYZE: EXECUTA a instrução (pede confirmação)")
	fs.BoolVar(&allowWrite, "allow-write", false, "permite ANALYZE de instruções que alteram dados (desfeitas com ROLLBACK)")
	fs.BoolVar(&allowRemote, "allow-remote", false, "permite servidores que não sejam locais")
	fs.BoolVar(&buffers, "buffers", false, "com --analyze: uso de buffers")
	fs.Var(&params, "param", "valor para $1, $2... (repetível); não é gravado")
	fs.DurationVar(&timeout, "timeout", 15*time.Second, "tempo limite do EXPLAIN")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fn == "" && !all {
		fs.Usage()
		return exitUsage
	}
	if err := e.load(dirArg(fs)); err != nil {
		return e.fail(err)
	}
	var res *sqlops.Result
	if all {
		res = &sqlops.Result{Symbol: evidence.SymbolRef{Qualified: "(all functions)"}}
		for _, p := range e.sess.Programs() {
			for _, op := range sqlops.All(p) {
				op.TraceParams(p, dataflow.Options{MaxDepth: 3, MaxNodes: 150})
				res.Ops = append(res.Ops, op)
			}
		}
		res.Status.Done()
	} else {
		r, err := e.symbol(fn)
		if err != nil {
			return e.fail(err)
		}
		res = e.sess.SQL(r, reachable, depth)
	}
	var plans []*pgexplain.Plan
	if explain {
		if dsn == "" {
			dsn = os.Getenv(pgexplain.EnvDSN)
		}
		plan, err := e.explain(res, opIdx, dsn, pgexplain.Config{AllowRemote: allowRemote, Timeout: timeout},
			pgexplain.Request{Analyze: analyze, AllowWrite: allowWrite, Params: params, Buffers: buffers})
		if err != nil {
			return e.fail(err)
		}
		plans = append(plans, plan)
	}
	if e.jsonOut {
		return e.printJSON(struct {
			*sqlops.Result
			Plans []*pgexplain.Plan `json:"plans,omitempty"`
		}{res, plans})
	}
	fmt.Fprint(e.stdout, renderSQL(res, nil).plain())
	for _, p := range plans {
		fmt.Fprintln(e.stdout)
		fmt.Fprint(e.stdout, renderPlan(p).plain())
	}
	return exitOK
}

func (e *inspectEnv) explain(res *sqlops.Result, opIdx int, dsn string, cfg pgexplain.Config, req pgexplain.Request) (*pgexplain.Plan, error) {
	var candidates []int
	for i, op := range res.Ops {
		if op.SQL.Text != "" {
			candidates = append(candidates, i)
		}
	}
	var op *sqlops.Op
	switch {
	case opIdx > 0 && opIdx <= len(res.Ops):
		op = res.Ops[opIdx-1]
	case opIdx > 0:
		return nil, fmt.Errorf("não há operação %d (a lista tem %d)", opIdx, len(res.Ops))
	case len(candidates) == 1:
		op = res.Ops[candidates[0]]
	case len(candidates) == 0:
		return nil, errors.New("nenhuma operação com SQL resolvido estaticamente para explicar")
	default:
		return nil, fmt.Errorf("%d operações têm SQL conhecido: escolha uma com --op N", len(candidates))
	}
	if op.SQL.Text == "" {
		return nil, errors.New(inspect.SQLUnresolved + ": não há texto completo para explicar")
	}
	req.SQL = op.SQL.Text
	cfg.DSN = dsn
	kind, err := pgexplain.Check(req)
	if err != nil {
		return nil, err
	}
	target, local, err := pgexplain.Target(dsn)
	if err != nil {
		return nil, err
	}
	if !local && !cfg.AllowRemote {
		return nil, fmt.Errorf("%s não é um servidor local; use --allow-remote somente para ambientes de desenvolvimento autorizados", target)
	}
	mode := "EXPLAIN (a instrução NÃO é executada; transação somente leitura, desfeita no fim)"
	if req.Analyze {
		mode = "EXPLAIN ANALYZE — a instrução SERÁ EXECUTADA"
		if !sqlops.ReadOnly(kind) {
			mode += " (altera dados; executada numa transação desfeita com ROLLBACK; efeitos fora da transação, como sequências, não são desfeitos)"
		} else {
			mode += " (transação somente leitura, desfeita no fim; funções chamadas pela instrução podem ter efeitos que a transação não impede)"
		}
	}
	lines := []string{"servidor: " + target, "modo: " + mode, "instrução (" + kind + "):"}
	for _, l := range strings.Split(sqlops.Format(req.SQL), "\n") {
		lines = append(lines, "    "+l)
	}
	if len(req.Params) > 0 {
		lines = append(lines, fmt.Sprintf("%d valor(es) de parâmetro (não serão gravados)", len(req.Params)))
	}
	if err := e.confirm(fmt.Sprintf("Plano de execução para %s %s em %s:", op.Lib, op.Op, loc(op.File, op.Line)), lines...); err != nil {
		return nil, err
	}
	return pgexplain.Explain(e.ctx, cfg, req)
}

// --- performance ---------------------------------------------------------------------

const perfUsage = `uso: gitview inspect performance [opções] [diretório]

Perfis (pprof) são amostrados: valores estimados. Benchmarks são medições
das execuções que os produziram. Diagnósticos do compilador são literais.

`

func (e *inspectEnv) performance(args []string) int {
	var fn, cpu, mem, sample, bench, pkg, benchtime, escape, compare, benchFile string
	var top, count int
	var flame bool
	var timeout time.Duration
	fs := e.flags("performance", perfUsage)
	fs.StringVar(&fn, "function", "", "foca uma função (custos, linhas, chamadores no perfil)")
	fs.StringVar(&fn, "f", "", "atalho para --function")
	fs.StringVar(&cpu, "cpu", "", "importa um perfil de CPU (pprof)")
	fs.StringVar(&mem, "memory", "", "importa um perfil de memória (pprof)")
	fs.StringVar(&sample, "sample", "", "tipo de amostra do perfil de memória: alloc_space, alloc_objects, inuse_space, inuse_objects")
	fs.IntVar(&top, "top", 20, "funções no ranking")
	fs.BoolVar(&flame, "flame", false, "mostra a árvore de chamadas (flame graph em texto)")
	fs.StringVar(&bench, "bench", "", "EXECUTA benchmarks que casam com a regexp (com -benchmem e perfis)")
	fs.StringVar(&pkg, "package", "", "pacote dos benchmarks, relativo ao módulo (padrão: o da --function)")
	fs.IntVar(&count, "count", 6, "repetições de cada benchmark (6+ permite teste de significância)")
	fs.StringVar(&benchtime, "benchtime", "", "valor de -benchtime")
	fs.StringVar(&benchFile, "bench-file", "", "importa uma saída de go test -bench")
	fs.StringVar(&escape, "escape", "", "compila o pacote com -gcflags=-m=2 e mostra os diagnósticos")
	fs.StringVar(&compare, "compare", "", "compara duas saídas de benchmark: antigo.txt,novo.txt")
	fs.DurationVar(&timeout, "timeout", 10*time.Minute, "tempo limite de benchmarks e builds")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if cpu == "" && mem == "" && bench == "" && escape == "" && compare == "" && benchFile == "" {
		fs.Usage()
		return exitUsage
	}
	if compare != "" && cpu == "" && mem == "" && bench == "" && escape == "" && fn == "" {
		return e.compare(compare)
	}
	if err := e.load(dirArg(fs)); err != nil {
		return e.fail(err)
	}
	progs := e.sess.Programs()
	var focus *inspect.Resolved
	if fn != "" {
		var err error
		if focus, err = e.symbol(fn); err != nil {
			return e.fail(err)
		}
	}
	focusID := ""
	if focus != nil {
		focusID = focus.Ref.ID
	}
	var out struct {
		CPU     *perf.Report       `json:"cpu,omitempty"`
		Mem     *perf.Report       `json:"memory,omitempty"`
		Bench   *perf.BenchRun     `json:"bench,omitempty"`
		Compare *perf.Comparison   `json:"compare,omitempty"`
		Escape  *perf.EscapeReport `json:"escape,omitempty"`
		Files   *perf.BenchFiles   `json:"files,omitempty"`
	}
	if cpu != "" {
		r, err := perf.Load(cpu, progs, perf.Options{})
		if err != nil {
			return e.fail(err)
		}
		out.CPU = r
	}
	if mem != "" {
		st := sample
		if st == "" {
			st = "alloc_space"
		}
		r, err := perf.Load(mem, progs, perf.Options{SampleType: st})
		if err != nil {
			return e.fail(err)
		}
		out.Mem = r
	}
	if benchFile != "" {
		b, err := perf.LoadBench(benchFile)
		if err != nil {
			return e.fail(err)
		}
		out.Bench = b
	}
	if bench != "" {
		p := pkg
		mod := progs[0]
		if focus != nil {
			mod = focus.Prog
			if p == "" {
				p = pkgPattern(focus)
			}
		} else if len(progs) > 1 {
			fmt.Fprintln(e.stderr, "gitview inspect performance: o repositório tem vários módulos; informe --function ou execute a partir do diretório do módulo")
			return exitUsage
		}
		if p == "" {
			fmt.Fprintln(e.stderr, "gitview inspect performance: informe --package ou --function")
			return exitUsage
		}
		dir := run.ArtifactPath(e.root, "bench")
		opt := perf.BenchOptions{Bench: bench, Count: count, Benchtime: benchtime, Profiles: true, OutDir: dir}
		spec, _ := perf.BenchSpec(mod.Dir, p, opt)
		rel, _ := filepath.Rel(e.root, mod.Dir)
		if err := e.confirm("Os benchmarks serão executados (código do repositório):", "$ "+spec.CommandLine(), "em "+rel, "resultados em "+dir); err != nil {
			return e.fail(err)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return e.fail(err)
		}
		c := perf.RunBench(e.ctx, progs, mod.Dir, p, opt, timeout)
		for _, msg := range c.Errors {
			fmt.Fprintln(e.stderr, "aviso:", msg)
		}
		if c.Run == nil {
			fmt.Fprint(e.stderr, string(c.Result.Stderr))
			return exitErr
		}
		out.Bench, out.CPU, out.Mem, out.Files = c.Run, c.CPU, c.Mem, &c.Files
		fmt.Fprintf(e.stderr, "saída: %s\nperfis: %s %s\n", c.Files.Output, c.Files.CPU, c.Files.Mem)
	}
	if escape != "" {
		mod := progs[0]
		if focus != nil {
			mod = focus.Prog
		} else if len(progs) > 1 {
			fmt.Fprintln(e.stderr, "gitview inspect performance: o repositório tem vários módulos; informe --function ou execute a partir do diretório do módulo")
			return exitUsage
		}
		spec := perf.EscapeSpec(mod.Dir, escape)
		spec.Timeout = timeout
		rel, _ := filepath.Rel(e.root, mod.Dir)
		if err := e.confirm("O pacote será compilado com diagnósticos do compilador (o binário é descartado; nada é executado):", "$ "+spec.CommandLine(), "em "+rel); err != nil {
			return e.fail(err)
		}
		res := run.Run(e.ctx, spec)
		if res.Err != nil {
			return e.fail(res.Err)
		}
		out.Escape = perf.ParseEscape(res.Stderr, progs)
		out.Escape.Artifact = evidence.Artifact{Kind: "escape", Name: "go build -gcflags=-m=2 " + escape, Command: spec.CommandLine(), CapturedAt: time.Now()}
		if res.ExitCode != 0 {
			out.Escape.BuildFailed()
			fmt.Fprintf(e.stderr, "go build terminou com código %d\n", res.ExitCode)
		}
	}
	if compare != "" {
		c, err := compareFiles(e.ctx, compare)
		if err != nil {
			return e.fail(err)
		}
		out.Compare = c
	}
	if e.jsonOut {
		return e.printJSON(out)
	}
	var parts []doc
	if out.CPU != nil {
		parts = append(parts, renderProfile(out.CPU, top, focusID))
		if flame {
			parts = append(parts, renderFlame(out.CPU, 12, 1))
		}
	}
	if out.Mem != nil {
		parts = append(parts, renderProfile(out.Mem, top, focusID))
		if flame {
			parts = append(parts, renderFlame(out.Mem, 12, 1))
		}
	}
	if out.Bench != nil {
		parts = append(parts, renderBench(out.Bench))
	}
	if out.Escape != nil {
		parts = append(parts, renderEscape(out.Escape, focusID, 0))
	}
	if out.Compare != nil {
		parts = append(parts, renderCompare(out.Compare))
	}
	for i, p := range parts {
		if i > 0 {
			fmt.Fprintln(e.stdout)
		}
		fmt.Fprint(e.stdout, p.plain())
	}
	return exitOK
}

func compareFiles(ctx context.Context, spec string) (*perf.Comparison, error) {
	old, cur, ok := strings.Cut(spec, ",")
	if !ok {
		return nil, errors.New("--compare espera antigo.txt,novo.txt")
	}
	o, err := perf.LoadBench(old)
	if err != nil {
		return nil, err
	}
	n, err := perf.LoadBench(cur)
	if err != nil {
		return nil, err
	}
	c := perf.Compare(o, n)
	// benchstat, when installed, only reads the two files.
	if path, err := exec.LookPath("benchstat"); err == nil {
		res := run.Run(ctx, run.Spec{Args: []string{path, old, cur}, Timeout: time.Minute})
		if res.Err == nil && res.ExitCode == 0 {
			c.Benchstat = string(res.Stdout)
		}
	} else {
		c.Method += " (benchstat is not installed; GitView computed the test itself)"
	}
	return c, nil
}

func (e *inspectEnv) compare(spec string) int {
	c, err := compareFiles(e.ctx, spec)
	if err != nil {
		return e.fail(err)
	}
	if e.jsonOut {
		return e.printJSON(c)
	}
	fmt.Fprint(e.stdout, renderCompare(c).plain())
	return exitOK
}

// --- trace ------------------------------------------------------------------------

const traceUsage = `uso: gitview inspect trace (--import trace.out | --test REGEXP --package P) [opções] [diretório]

Mostra eventos observados: goroutines, bloqueios, GC, tarefas, regiões e
logs. O trace não registra cada chamada de função: duração por função só
existe em regiões (do próprio código ou da instrumentação --instrument,
aplicada temporariamente via go test -overlay, sem alterar arquivos).

`

func (e *inspectEnv) trace(args []string) int {
	var imp, test, pkg, instrument, fn string
	var width int
	var all bool
	var timeout time.Duration
	fs := e.flags("trace", traceUsage)
	fs.StringVar(&imp, "import", "", "importa um arquivo de trace")
	fs.StringVar(&test, "test", "", "EXECUTA os testes que casam com a regexp gravando um trace")
	fs.StringVar(&pkg, "package", "", "pacote dos testes, relativo ao módulo (padrão: o da --function)")
	fs.StringVar(&instrument, "instrument", "", "funções a medir com regiões temporárias (lista separada por vírgulas)")
	fs.StringVar(&fn, "function", "", "foca uma função (goroutines, spans, bloqueios)")
	fs.StringVar(&fn, "f", "", "atalho para --function")
	fs.IntVar(&width, "width", 100, "largura da linha do tempo em texto")
	fs.BoolVar(&all, "all-goroutines", false, "inclui goroutines do runtime na linha do tempo")
	fs.DurationVar(&timeout, "timeout", 5*time.Minute, "tempo limite dos testes")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if imp == "" && test == "" {
		fs.Usage()
		return exitUsage
	}
	if err := e.load(dirArg(fs)); err != nil {
		return e.fail(err)
	}
	progs := e.sess.Programs()
	var focus *inspect.Resolved
	if fn != "" {
		var err error
		if focus, err = e.symbol(fn); err != nil {
			return e.fail(err)
		}
	}
	var t *xtrace.Timeline
	if imp != "" {
		var err error
		if t, err = xtrace.Load(imp, progs, xtrace.Limits{}); err != nil {
			return e.fail(err)
		}
	} else {
		mod := progs[0]
		p := pkg
		if focus != nil {
			mod = focus.Prog
			if p == "" {
				p = pkgPattern(focus)
			}
		} else if len(progs) > 1 {
			fmt.Fprintln(e.stderr, "gitview inspect trace: o repositório tem vários módulos; informe --function ou execute a partir do diretório do módulo")
			return exitUsage
		}
		if p == "" {
			fmt.Fprintln(e.stderr, "gitview inspect trace: informe --package ou --function")
			return exitUsage
		}
		var ids []string
		for _, q := range strings.Split(instrument, ",") {
			if q = strings.TrimSpace(q); q == "" {
				continue
			}
			r, err := e.symbol(q)
			if err != nil {
				return e.fail(err)
			}
			ids = append(ids, r.Ref.ID)
		}
		dir := run.ArtifactPath(e.root, "trace")
		pl, err := xtrace.PlanCapture(mod, xtrace.CaptureOptions{Pkg: p, Run: test, Instrument: ids, OutDir: dir, Timeout: timeout})
		if err != nil {
			return e.fail(err)
		}
		rel, _ := filepath.Rel(e.root, mod.Dir)
		lines := []string{"$ " + pl.Spec.CommandLine(), "em " + rel, "trace em " + pl.TraceFile}
		for _, id := range pl.Instrumented {
			lines = append(lines, "região temporária em "+id+" (cópia via -overlay; arquivos do projeto não são alterados)")
		}
		for _, s := range pl.Skipped {
			lines = append(lines, "não instrumentada: "+s)
		}
		if err := e.confirm("Os testes serão executados com trace (código do repositório, possíveis efeitos externos):", lines...); err != nil {
			return e.fail(err)
		}
		c := xtrace.RunCapture(e.ctx, pl, progs)
		if c.Err != nil {
			fmt.Fprint(e.stderr, string(c.Result.Stderr))
			return e.fail(c.Err)
		}
		t = c.Timeline
		fmt.Fprintln(e.stderr, "trace gravado em", pl.TraceFile)
	}
	if e.jsonOut {
		return e.printJSON(t)
	}
	focusID := ""
	if focus != nil {
		focusID = focus.Ref.ID
	}
	fmt.Fprint(e.stdout, renderTrace(t, focusID, width, !all, 0, 0).plain())
	return exitOK
}

// --- report -----------------------------------------------------------------------

const reportUsage = `uso: gitview inspect report -o relatório.html [opções] [diretório]

Gera um relatório independente (HTML ou JSON) com o Go Deep Inspector:
funções escolhidas (ou as modificadas no working tree), Code Flow e os
dados de execução importados. O HTML funciona offline.

`

func (e *inspectEnv) report(args []string) int {
	var fns, out, cpu, mem, tr, cover, bench, format, escLog string
	var overwrite, withDiff, withStudy bool
	fs := e.flags("report", reportUsage)
	fs.BoolVar(&withStudy, "study", false, "inclui a aba Study (por onde começar, arquitetura, jornada da alteração)")
	fs.StringVar(&escLog, "escape-log", "", "saída salva de go build -gcflags=-m=2 (diagnósticos do compilador)")
	fs.StringVar(&fns, "function", "", "funções a incluir (lista separada por vírgulas); padrão: as modificadas")
	fs.StringVar(&fns, "f", "", "atalho para --function")
	fs.StringVar(&out, "o", "", "arquivo de saída (.html ou .json)")
	fs.StringVar(&cpu, "cpu", "", "perfil de CPU")
	fs.StringVar(&mem, "memory", "", "perfil de memória")
	fs.StringVar(&tr, "trace", "", "trace de execução")
	fs.StringVar(&cover, "cover", "", "perfil de cobertura")
	fs.StringVar(&bench, "bench-file", "", "saída de benchmarks")
	fs.StringVar(&format, "format", "", "html ou json (padrão: pela extensão)")
	fs.BoolVar(&overwrite, "overwrite", false, "substitui o arquivo de saída se existir")
	fs.BoolVar(&withDiff, "diff", false, "inclui os diffs do working tree (permite \"Open in diff\")")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if out == "" {
		fs.Usage()
		return exitUsage
	}
	if err := e.load(dirArg(fs)); err != nil {
		return e.fail(err)
	}
	if err := e.loadArtifacts(cpu, mem, tr, cover, bench); err != nil {
		return e.fail(err)
	}
	if escLog != "" {
		data, err := os.ReadFile(escLog)
		if err != nil {
			return e.fail(err)
		}
		e.sess.Escape = perf.ParseEscape(data, e.sess.Programs())
		e.sess.Escape.Artifact = evidence.Artifact{Kind: "escape", Name: filepath.Base(escLog), Path: escLog, Imported: true}
	}
	var targets []*inspect.Resolved
	for _, q := range strings.Split(fns, ",") {
		if q = strings.TrimSpace(q); q == "" {
			continue
		}
		r, err := e.symbol(q)
		if err != nil {
			return e.fail(err)
		}
		targets = append(targets, r)
	}
	changed := map[string]map[int]bool{}
	var files []FileChange
	if repo, err := OpenRepo(e.root); err == nil {
		if fc, err := repo.Changes(); err == nil {
			files = fc
			changed = changedFromFiles(fc, func(f FileChange) *parsedDiff { return parseDiff(f.Path, f.Diff) })
		}
	}
	if len(targets) == 0 {
		targets = e.sess.Changed(changed)
	}
	f := export.HTML
	if strings.EqualFold(format, "json") || format == "" && strings.HasSuffix(strings.ToLower(out), ".json") {
		f = export.JSON
	}
	rep := export.Report{Project: filepath.Base(e.root), Root: e.root, GeneratedAt: time.Now(), Scope: "Go Deep Inspector",
		Tests: &export.TestReport{Ran: false}}
	if repo, err := OpenRepo(e.root); err == nil {
		rep.Branch = repo.Branch()
		if c := repo.HeadCommit(); c != nil {
			rep.Base = &export.Commit{Hash: c.Hash, Short: c.Short, Subject: c.Subject, Author: c.Author, Date: c.Date}
		}
	}
	allFiles := files
	if !withDiff {
		files = nil
	}
	for _, fc := range files {
		pd := parseDiff(fc.Path, fc.Diff)
		gf := gitdiffFile(fc)
		if pd != nil {
			gf.Hunks = pd.hunks
		}
		rep.Files = append(rep.Files, gf)
	}
	rep.Files, rep.Omitted = export.Redact(rep.Files, false)
	if withStudy {
		buildStudyReport(e.ctx, &rep, e.root, e.sess.Programs(), nil, changed, allFiles, e.sess, targets)
	} else {
		rep.Flow = flowIndex(e.sess.Programs(), changed, nil)
		rep.Inspect = buildInspectData(e.sess, targets)
	}
	if f == export.HTML {
		rep.Hovers = offlineHovers(e.sess.Programs(), &rep)
	}
	data, err := export.Render(f, rep)
	if err != nil {
		return e.fail(err)
	}
	if err := export.WriteFile(out, data, overwrite); err != nil {
		if errors.Is(err, export.ErrExists) {
			fmt.Fprintln(e.stderr, "gitview inspect report:", out, "já existe; use --overwrite")
			return exitErr
		}
		return e.fail(err)
	}
	fmt.Fprintf(e.stderr, "relatório gravado em %s (%d função(ões))\n", out, len(rep.Inspect.Symbols))
	return exitOK
}
