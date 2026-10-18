package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/athos/gitview/internal/export"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/graph/builder"
	gexport "github.com/athos/gitview/internal/graph/export"
	gmodel "github.com/athos/gitview/internal/graph/model"
)

const graphUsage = `uso: gitview graph <subcomando> [opções] [diretório]

Repository Graph: o repositório Go inteiro como um grafo navegável (pacotes,
arquivos, tipos, funções, chamadas, imports, implementações, rotas e SQL),
obtido por análise estática com go/packages, go/types e SSA. Nada é
executado e nenhuma relação é deduzida por nome.

subcomandos:
  export   grava o grafo: html (padrão, com o HTTP Explorer), json,
           openapi (OpenAPI 3.1) ou postman (Postman Collection v2.1)
  routes   lista as rotas HTTP encontradas, com handler e contrato
  journey  mostra o caminho de uma rota pelo código e o seu contrato
  stats    imprime as estatísticas da análise

exemplos:
  gitview graph export                          # gitview-graph.html
  gitview graph export --output architecture.html
  gitview graph export --format json --output graph.json
  gitview graph export --format json --level packages
  gitview graph export --format openapi --output openapi.json
  gitview graph export --format postman --output collection.json
  gitview graph export --level packages         # HTML: abre com todos os pacotes visíveis
  gitview graph export --include-http=false     # sem o catálogo HTTP
  gitview graph export --include-source=false   # HTML sem o código-fonte (aba Source vazia)
  gitview graph routes
  gitview graph routes --format json
  gitview graph journey --route "POST /api/clients" --depth 4
  gitview graph stats --module backend

níveis (--level): full, packages, files, functions, types.
No HTML o nível é só a visão inicial do canvas (packages expande os diretórios,
files expande também os pacotes): o arquivo contém o grafo completo.
No JSON o nível filtra o dataset (files acrescenta arestas derivadas entre
arquivos, marcadas como "derived").

Contratos HTTP vêm da análise estática dos handlers: um caminho de chamadas
é um caminho possível, não a execução de uma requisição. Os exemplos de
payload e os comandos cURL usam valores sintéticos e nunca são executados.
`

type graphFlags struct {
	format      string
	output      string
	level       string
	module      string
	maxNodes    int
	maxEdges    int
	overwrite   bool
	jsonOut     bool
	includeHTTP bool
	includeSrc  bool
	route       string
	depth       int
}

var exportFormats = map[string]string{"html": "gitview-graph.html", "json": "", "openapi": "", "postman": ""}

func runGraph(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stderr, graphUsage)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	sub, args := args[0], args[1:]
	switch sub {
	case "export", "stats", "routes", "journey":
	default:
		fmt.Fprintf(stderr, "gitview graph: subcomando desconhecido %q\n\n%s", sub, graphUsage)
		return 2
	}
	fs := flag.NewFlagSet("graph "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var f graphFlags
	fs.StringVar(&f.module, "module", "", "analisa só o módulo Go deste diretório (padrão: todos os módulos do repositório)")
	switch sub {
	case "export":
		fs.StringVar(&f.format, "format", "html", "html, json, openapi ou postman")
		fs.StringVar(&f.output, "output", "", "arquivo de saída (padrão gitview-graph.html; json, openapi e postman sem --output vão para a saída padrão)")
		fs.StringVar(&f.output, "o", "", "atalho para --output")
		fs.StringVar(&f.level, "level", "", "full, packages, files, functions ou types")
		fs.IntVar(&f.maxNodes, "max-nodes", 5000, "HTML: elementos que uma expansão pode desenhar; acima disso a página sugere abrir o grupo como escopo")
		fs.IntVar(&f.maxEdges, "max-edges", 25000, "HTML: mantido por compatibilidade (o canvas agrega as relações por grupo)")
		fs.BoolVar(&f.overwrite, "overwrite", false, "substitui o arquivo de saída se existir")
		fs.BoolVar(&f.includeHTTP, "include-http", true, "inclui o catálogo HTTP (rotas e contratos) e o HTTP Explorer")
		fs.BoolVar(&f.includeSrc, "include-source", true, "HTML: embute os arquivos .go do repositório (compactados) para a aba Source")
	case "routes":
		fs.StringVar(&f.format, "format", "text", "text ou json")
	case "journey":
		fs.StringVar(&f.route, "route", "", `rota: "MÉTODO /caminho" como registrado, ou o ID completo`)
		fs.IntVar(&f.depth, "depth", 4, "chamadas seguidas a partir da cadeia de handlers (1-10)")
		fs.BoolVar(&f.jsonOut, "json", false, "imprime o resultado em JSON")
	default:
		fs.BoolVar(&f.jsonOut, "json", false, "imprime as estatísticas em JSON")
	}
	fs.Usage = func() { fmt.Fprint(stderr, graphUsage+"\nopções:\n"); fs.PrintDefaults() }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if _, ok := exportFormats[f.format]; sub == "export" && !ok {
		fmt.Fprintln(stderr, "gitview graph export: --format deve ser html, json, openapi ou postman")
		return 2
	}
	if sub == "routes" && f.format != "text" && f.format != "json" {
		fmt.Fprintln(stderr, "gitview graph routes: --format deve ser text ou json")
		return 2
	}
	if f.level != "" && !gexport.ValidLevel(f.level) {
		fmt.Fprintln(stderr, "gitview graph export: --level deve ser um de", strings.Join(gexport.Levels, ", "))
		return 2
	}
	if sub == "journey" && (f.route == "" || f.depth < 1 || f.depth > 10) {
		fmt.Fprintln(stderr, "gitview graph journey: informe --route e uma --depth entre 1 e 10")
		return 2
	}
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	noHTTP := sub == "export" && !f.includeHTTP
	if sub == "export" && !f.includeHTTP && (f.format == "openapi" || f.format == "postman") {
		fmt.Fprintln(stderr, "gitview graph export: --format", f.format, "precisa do catálogo HTTP")
		return 2
	}
	g, err := analyzeRepoGraph(ctx, dir, f.module, stderr, noHTTP)
	if err != nil {
		fmt.Fprintln(stderr, "gitview graph:", err)
		if errors.Is(err, context.Canceled) {
			return 130
		}
		return 1
	}
	switch sub {
	case "stats":
		if f.jsonOut {
			return writeOrFail(stdout, stderr, func(w io.Writer) error { return gexport.JSON(w, &gmodel.Graph{Schema: g.Schema, Stats: g.Stats}, "") })
		}
		printGraphStats(stdout, g)
		return 0
	case "routes":
		if f.format == "json" {
			if g.HTTP == nil {
				g.HTTP = &gmodel.HTTP{Routes: []*gmodel.Route{}}
			}
			return writeOrFail(stdout, stderr, func(w io.Writer) error {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(g.HTTP)
			})
		}
		printRoutes(stdout, g)
		return 0
	case "journey":
		return runJourney(stdout, stderr, g, f.route, f.depth, f.jsonOut)
	}
	printGraphStats(stderr, g)

	var buf bytes.Buffer
	switch f.format {
	case "json":
		err = gexport.JSON(&buf, g, f.level)
	case "openapi":
		err = gexport.OpenAPI(&buf, g)
	case "postman":
		err = gexport.Postman(&buf, g)
	default:
		err = gexport.HTML(&buf, g, gexport.HTMLOptions{Level: f.level, MaxNodes: f.maxNodes, MaxEdges: f.maxEdges, NoSource: !f.includeSrc})
	}
	if err != nil {
		fmt.Fprintln(stderr, "gitview graph export:", err)
		return 1
	}
	out := f.output
	if out == "" && exportFormats[f.format] == "" {
		_, err := stdout.Write(buf.Bytes())
		if err != nil {
			return 1
		}
		return 0
	}
	if out == "" {
		out = exportFormats[f.format]
	}
	if err := export.WriteFile(out, buf.Bytes(), f.overwrite); err != nil {
		if errors.Is(err, export.ErrExists) {
			fmt.Fprintln(stderr, "gitview graph export:", out, "já existe; use --overwrite")
			return 1
		}
		fmt.Fprintln(stderr, "gitview graph export:", err)
		return 1
	}
	fmt.Fprintf(stderr, "grafo gravado em %s (%s)\n", out, byteSize(buf.Len()))
	return 0
}

func writeOrFail(w, stderr io.Writer, fn func(io.Writer) error) int {
	if err := fn(w); err != nil {
		fmt.Fprintln(stderr, "gitview graph:", err)
		return 1
	}
	return 0
}

// graphModules returns the repository root and the module directories to
// analyse: every module of the repository, or only the one containing
// module when it is given.
func graphModules(dir, module string) (string, []string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, err
	}
	root := abs
	if repo, err := OpenRepo(abs); err == nil {
		root = repo.Root
	}
	if module != "" {
		m, err := filepath.Abs(module)
		if err != nil {
			return "", nil, err
		}
		mod := flow.EnclosingModule(m, root)
		if mod == "" {
			return "", nil, fmt.Errorf("nenhum go.mod em %s", module)
		}
		return root, []string{mod}, nil
	}
	mods := flow.FindModules(root)
	if len(mods) == 0 {
		if m := flow.EnclosingModule(abs, root); m != "" {
			mods = []string{m}
		}
	}
	if len(mods) == 0 {
		return "", nil, fmt.Errorf("nenhum módulo Go (go.mod) em %s", root)
	}
	return root, mods, nil
}

// analyzeRepoGraph loads the modules (printing progress) and builds the
// dataset.
func analyzeRepoGraph(ctx context.Context, dir, module string, progress io.Writer, noHTTP bool) (*gmodel.Graph, error) {
	root, progs, failures, took, err := loadGraphPrograms(ctx, dir, module, progress)
	if err != nil {
		return nil, err
	}
	return buildRepoGraph(ctx, root, progs, failures, took, noHTTP)
}

// loadGraphPrograms loads every module of the repository (or one) with
// Code Flow; modules that fail are listed in failures.
func loadGraphPrograms(ctx context.Context, dir, module string, progress io.Writer) (root string, progs []*flow.Program, failures []string, took time.Duration, err error) {
	root, mods, err := graphModules(dir, module)
	if err != nil {
		return "", nil, nil, 0, err
	}
	start := time.Now()
	for _, m := range mods {
		rel, _ := filepath.Rel(root, m)
		fmt.Fprintf(progress, "analisando %s…", rel)
		t := time.Now()
		p, err := flow.Load(ctx, root, m)
		if ctx.Err() != nil {
			fmt.Fprintln(progress)
			return "", nil, nil, 0, ctx.Err()
		}
		if err != nil {
			fmt.Fprintln(progress, " falhou:", err)
			failures = append(failures, rel+": "+err.Error())
			continue
		}
		fmt.Fprintf(progress, " %s\n", time.Since(t).Round(10*time.Millisecond))
		progs = append(progs, p)
	}
	return root, progs, failures, time.Since(start), nil
}

func buildRepoGraph(ctx context.Context, root string, progs []*flow.Program, failures []string, analysis time.Duration, noHTTP bool) (*gmodel.Graph, error) {
	if len(progs) == 0 {
		if len(failures) > 0 {
			return nil, errors.New(failures[0])
		}
		return nil, errors.New("nenhum pacote Go analisado")
	}
	g, err := builder.Build(ctx, progs, builder.Options{Repository: filepath.Base(root), Root: root, Analysis: analysis, NoHTTP: noHTTP})
	if err != nil {
		return nil, err
	}
	for _, f := range failures {
		g.Completeness.Limit("módulo não analisado: " + f)
	}
	if err := g.Validate(); err != nil {
		return nil, fmt.Errorf("dataset inconsistente (erro interno): %w", err)
	}
	return g, nil
}

// graphStatsLines renders the build metrics, shared by the CLI and the TUI.
func graphStatsLines(g *gmodel.Graph) [][2]string {
	s := g.Stats
	return [][2]string{
		{"Modules analyzed", fmt.Sprint(s.Modules)},
		{"Packages analyzed", groupDigits(s.Packages)},
		{"External packages", groupDigits(s.ExternalPackages)},
		{"Files analyzed", groupDigits(s.Files)},
		{"Functions discovered", groupDigits(s.Functions)},
		{"Types discovered", groupDigits(s.Types)},
		{"Nodes generated", groupDigits(s.Nodes)},
		{"Edges generated", groupDigits(s.Edges)},
		{"Communities detected", groupDigits(s.Communities)},
		{"HTTP routes", groupDigits(s.Routes)},
		{"Package modularity", fmt.Sprintf("%.3f", s.Modularity)},
		{"Calls by confidence", fmt.Sprintf("%d confirmed, %d likely, %d possible, %d unresolved",
			s.EdgesByConf[gmodel.Confirmed], s.EdgesByConf[gmodel.Likely], s.EdgesByConf[gmodel.Possible], s.EdgesByConf[gmodel.Unresolved])},
		{"Analysis duration", (time.Duration(s.AnalysisMS) * time.Millisecond).String()},
		{"Graph build", (time.Duration(s.BuildMS) * time.Millisecond).String()},
		{"Communities + layout", (time.Duration(s.LayoutMS) * time.Millisecond).String()},
	}
}

func printGraphStats(w io.Writer, g *gmodel.Graph) {
	for _, l := range graphStatsLines(g) {
		fmt.Fprintf(w, "%-22s %s\n", l[0]+":", l[1])
	}
	if g.Completeness.Status != "complete" {
		fmt.Fprintf(w, "%-22s %s\n", "Completeness:", g.Completeness.String())
	}
}

func groupDigits(n int) string {
	s := fmt.Sprint(n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func byteSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
