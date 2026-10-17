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
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/athos/gitview/internal/config"
	"github.com/athos/gitview/internal/export"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/live/capture"
	"github.com/athos/gitview/internal/live/goprof"
	"github.com/athos/gitview/internal/live/httpcol"
	"github.com/athos/gitview/internal/live/proc"
	"github.com/athos/gitview/internal/live/report"
	"github.com/athos/gitview/internal/live/srcmap"
)

const runtimeUsage = `uso: gitview runtime <subcomando> [opções]

Runtime Performance Inspector: mede um processo Go em execução a partir do
PID, sem benchmarks. Envie as requisições pelo Postman/curl durante a
captura; o GitView registra o processo e correlaciona as requisições.

  gitview runtime ps                               processos Go deste usuário
  gitview runtime inspect <pid>                    o que pode ser medido no processo
  gitview runtime capture <pid>                    captura até Enter/Ctrl+C, com proxy local
  gitview runtime capture <pid> --duration 30s --profile cpu,heap --yes
  gitview runtime history                          capturas salvas deste repositório
  gitview runtime show <id> [-o relatorio.html]    relatório de uma captura
  gitview runtime compare <idA> <idB> [-o comparacao.html]
  gitview runtime delete <id>

Fontes de dados (todas locais):
  /proc            CPU, memória, threads, E/S do processo (sempre)
  proxy local      127.0.0.1 → aplicação: método, rota, status, tempo, bytes
  collector        github.com/athos/gitview/collector na aplicação: padrão de
                   rota, SQL com linha de origem, CPU por requisição (opcional)
  net/http/pprof   perfis e execution trace, quando a aplicação os serve
  OpenTelemetry    spans OTLP/HTTP recebidos em 127.0.0.1:4318 (--otlp)

Métricas do processo valem para o processo inteiro; números por requisição
são marcados como amostrados ou estimados. Profiling exige confirmação
(--yes em modo não interativo). GITVIEW_RUNTIME=off desativa o inspetor.
`

type runtimeEnv struct {
	stdout, stderr io.Writer
	stdin          io.Reader
	ctx            context.Context
	dir            string
	jsonOut        bool
	yes            bool
	nonInter       bool
	noSource       bool
	out            string
}

func runRuntime(args []string, stdout, stderr io.Writer) int {
	return runRuntimeIO(args, stdout, stderr, os.Stdin)
}

func runRuntimeIO(args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	if capture.Disabled() {
		fmt.Fprintln(stderr, "gitview runtime: desativado (GITVIEW_RUNTIME=off)")
		return exitErr
	}
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	env := &runtimeEnv{stdout: stdout, stderr: stderr, stdin: stdin, ctx: context.Background()}
	switch sub {
	case "ps", "list":
		return env.ps(args)
	case "inspect":
		return env.inspect(args)
	case "capture", "record":
		return env.capture(args)
	case "history":
		return env.history(args)
	case "show", "report":
		return env.show(args)
	case "compare", "diff":
		return env.compare(args)
	case "delete", "rm":
		return env.delete(args)
	case "", "help", "-h", "--help":
		fmt.Fprint(stdout, runtimeUsage)
		if sub == "" {
			return exitUsage
		}
		return exitOK
	}
	fmt.Fprintf(stderr, "gitview runtime: subcomando desconhecido %q\n\n%s", sub, runtimeUsage)
	return exitUsage
}

func (e *runtimeEnv) flags(name, usage string) *flag.FlagSet {
	fs := flag.NewFlagSet("runtime "+name, flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	fs.BoolVar(&e.jsonOut, "json", false, "saída em JSON")
	fs.StringVar(&e.dir, "repo", ".", "repositório cujo código é relacionado às medições")
	fs.Usage = func() { fmt.Fprint(e.stderr, usage); fs.PrintDefaults() }
	return fs
}

func (e *runtimeEnv) fail(err error) int {
	// Errors may quote replies of the monitored application.
	fmt.Fprintln(e.stderr, "gitview runtime:", cleanText(err.Error()))
	switch {
	case errors.Is(err, errConsent):
		return exitConsent
	case errors.Is(err, context.Canceled):
		return 130
	}
	return exitErr
}

func (e *runtimeEnv) printJSON(v any) int {
	enc := json.NewEncoder(e.stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return e.fail(err)
	}
	return exitOK
}

// parseFlags parses flags placed before or after positional arguments.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// repoRoot returns the repository root of --repo, or "" outside one.
func (e *runtimeEnv) repoRoot() string {
	abs, err := filepath.Abs(e.dir)
	if err != nil {
		return ""
	}
	if r, err := OpenRepo(abs); err == nil {
		return r.Root
	}
	return ""
}

// store opens the history with the repository's (tighter) limits.
func (e *runtimeEnv) store(root string) (*capture.Store, error) {
	st, err := capture.StoreFor(root)
	if err != nil {
		return nil, err
	}
	if root != "" {
		cfg, err := config.Load(root)
		if err != nil {
			return nil, err
		}
		if cfg.Runtime.Disabled {
			return nil, errors.New("o Runtime Performance Inspector está desativado em " + config.FileName + " (runtime.disabled)")
		}
		applyRuntimeConfig(&st.Limits, cfg.Runtime)
	}
	return st, nil
}

// applyRuntimeConfig lowers the limits; a repository cannot raise them.
func applyRuntimeConfig(l *capture.Limits, c config.RuntimeConfig) {
	if c.MaxCaptures > 0 && c.MaxCaptures < l.MaxCaptures {
		l.MaxCaptures = c.MaxCaptures
	}
	if c.MaxStorageMB > 0 && int64(c.MaxStorageMB)<<20 < l.MaxBytes {
		l.MaxBytes = int64(c.MaxStorageMB) << 20
	}
	if d, err := time.ParseDuration(c.MaxDuration); err == nil && d > 0 && d < l.MaxDuration {
		l.MaxDuration = d
	}
	// Sampling less often is the only direction a repository may choose.
	if d, err := time.ParseDuration(c.Interval); err == nil && d > l.DefaultEvery && d <= l.MaxInterval {
		l.DefaultEvery = d
		l.MinInterval = d
	}
}

// mapper loads the repository's Go code for source mapping: the module
// that built the process when the repository contains it (same module
// path as the binary's build information), else the module of --repo.
func (e *runtimeEnv) mapper(root, mainModule string) *srcmap.Mapper {
	if e.noSource || root == "" {
		return nil
	}
	dir := e.dir
	if mainModule != "" {
		abs, _ := filepath.Abs(e.dir)
		mods := flow.FindModules(root)
		if m := flow.EnclosingModule(abs, root); m != "" {
			mods = append([]string{m}, mods...)
		}
		for _, m := range mods {
			if modulePath(m) == mainModule {
				dir = m
				break
			}
		}
	}
	r, progs, err := loadPrograms(e.ctx, dir, e.stderr)
	if err != nil {
		fmt.Fprintln(e.stderr, "código-fonte não relacionado:", err)
		return nil
	}
	return srcmap.New(r, progs)
}

// modulePath reads the module path of dir/go.mod.
func modulePath(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module"); ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t') {
			rest = strings.TrimSpace(rest)
			if i := strings.Index(rest, "//"); i >= 0 {
				rest = strings.TrimSpace(rest[:i])
			}
			return strings.Trim(rest, "\"`")
		}
	}
	return ""
}

func pidArg(pos []string) (int, error) {
	if len(pos) != 1 {
		return 0, errors.New("informe um PID")
	}
	pid, err := strconv.Atoi(pos[0])
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("PID inválido %q", pos[0])
	}
	return pid, nil
}

func (e *runtimeEnv) ps(args []string) int {
	fs := e.flags("ps", "uso: gitview runtime ps [--all] [--json]\n")
	all := fs.Bool("all", false, "inclui processos que não são Go")
	if _, err := parseFlags(fs, args); err != nil {
		return exitUsage
	}
	if err := proc.Supported(); err != nil {
		return e.fail(err)
	}
	list, err := proc.Default.List(!*all)
	if err != nil {
		return e.fail(err)
	}
	if e.jsonOut {
		return e.printJSON(list)
	}
	fmt.Fprint(e.stdout, renderProcList(list).plain())
	return exitOK
}

func (e *runtimeEnv) inspect(args []string) int {
	fs := e.flags("inspect", "uso: gitview runtime inspect <pid> [--json]\n\nLê /proc e procura o collector do GitView e net/http/pprof nas portas do\nprocesso (GET /debug/pprof/ e /debug/pprof/cmdline, só leitura).\n\n")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return exitUsage
	}
	pid, err := pidArg(pos)
	if err != nil {
		return e.fail(err)
	}
	tg, err := capture.Prepare(e.ctx, proc.Default, pid)
	if err != nil {
		return e.fail(err)
	}
	if e.jsonOut {
		type ep struct {
			Kind    string `json:"kind"`
			Addr    string `json:"addr"`
			Exposed bool   `json:"exposed"`
		}
		out := struct {
			Process   *proc.Process          `json:"process"`
			Collector *httpcol.CollectorInfo `json:"collector,omitempty"`
			Endpoints []ep                   `json:"endpoints"`
			Notes     []string               `json:"notes"`
		}{Process: tg.Process, Notes: tg.Notes, Endpoints: []ep{}}
		if tg.Collector != nil {
			out.Collector = &tg.CollInfo
		}
		for _, x := range tg.Endpoints {
			out.Endpoints = append(out.Endpoints, ep{x.Kind, x.Addr, x.Exposed})
		}
		return e.printJSON(out)
	}
	fmt.Fprint(e.stdout, renderTarget(tg).plain())
	return exitOK
}

// profilePlan turns --profile into a plan.
func profilePlan(list string, cpuWindow time.Duration, traceSecs int, noGC, setRates bool) (goprof.Plan, error) {
	pl := goprof.Plan{CPUWindow: cpuWindow, TraceSeconds: traceSecs, HeapGC: !noGC, SetRates: setRates}
	if list == "" {
		return pl, nil
	}
	for _, k := range strings.Split(list, ",") {
		switch strings.TrimSpace(strings.ToLower(k)) {
		case "cpu":
			pl.CPU = true
		case "heap", "mem", "memory", "allocs":
			pl.Heap = true
		case "goroutine", "goroutines":
			pl.Goroutine = true
		case "mutex":
			pl.Mutex = true
		case "block":
			pl.Block = true
		case "trace":
			pl.Trace = true
		case "runtime", "metrics":
			pl.Runtime = true
		case "all":
			pl.CPU, pl.Heap, pl.Goroutine, pl.Mutex, pl.Block = true, true, true, true, true
		case "":
		default:
			return pl, fmt.Errorf("perfil desconhecido %q (cpu, heap, goroutine, mutex, block, trace, runtime, all)", k)
		}
	}
	return pl, nil
}

func (e *runtimeEnv) capture(args []string) int {
	fs := e.flags("capture", `uso: gitview runtime capture <pid> [opções]

Inicia a captura: amostra o processo, sobe o proxy local (se houver porta) e
arma o collector, se a aplicação o tiver. A fase ativa termina com Enter,
Ctrl+C (Ctrl+C duas vezes cancela sem salvar) ou --duration.

`)
	dur := fs.Duration("duration", 0, "duração da fase ativa (0: até Enter/Ctrl+C)")
	interval := fs.Duration("interval", 0, "intervalo de amostragem do processo (padrão 250ms; 50ms–5s)")
	baseline := fs.Duration("baseline", 2*time.Second, "medição antes da fase ativa")
	cooldown := fs.Duration("cooldown", 2*time.Second, "medição depois da fase ativa")
	label := fs.String("label", "", "nome da captura (ex.: \"antes do índice\")")
	noProxy := fs.Bool("no-proxy", false, "não sobe o proxy local")
	proxyListen := fs.String("listen", "127.0.0.1:0", "endereço do proxy (somente loopback)")
	target := fs.String("target", "", "endereço da aplicação atrás do proxy (padrão: porta do processo)")
	noColl := fs.Bool("no-collector", false, "não usa o collector do GitView mesmo que exista")
	otlp := fs.String("otlp", "", "recebe spans OpenTelemetry neste endereço (ex.: 127.0.0.1:4318)")
	otlpAny := fs.Bool("otlp-any-process", false, "aceita spans sem process.pid (podem ser de outro serviço local)")
	profiles := fs.String("profile", "", "perfis: cpu,heap,goroutine,mutex,block,trace,runtime ou all (exige confirmação)")
	cpuWindow := fs.Duration("cpu-window", 2*time.Second, "duração de cada janela do perfil de CPU")
	traceSecs := fs.Int("trace-seconds", 5, "duração do execution trace (máx. 30)")
	noGC := fs.Bool("no-gc", false, "não executa GC antes dos snapshots de heap")
	setRates := fs.Bool("set-rates", false, "liga perfis de mutex/bloqueio durante a captura (só com o collector)")
	endpoint := fs.String("endpoint", "", "rota de interesse no relatório (ex.: \"GET /clients/{id}\")")
	fs.BoolVar(&e.yes, "yes", false, "confirma o profiling sem perguntar")
	fs.BoolVar(&e.nonInter, "non-interactive", false, "nunca pergunta")
	fs.BoolVar(&e.noSource, "no-source", false, "não relaciona as medições ao código do repositório")
	fs.StringVar(&e.out, "o", "", "grava também o relatório HTML neste arquivo")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return exitUsage
	}
	pid, err := pidArg(pos)
	if err != nil {
		return e.fail(err)
	}
	root := e.repoRoot()
	st, err := e.store(root)
	if err != nil {
		return e.fail(err)
	}
	if *dur > st.Limits.MaxDuration {
		return e.fail(fmt.Errorf("--duration acima do limite de %s", st.Limits.MaxDuration))
	}
	plan, err := profilePlan(*profiles, *cpuWindow, *traceSecs, *noGC, *setRates)
	if err != nil {
		return e.fail(err)
	}
	if plan.SetRates && !plan.Mutex && !plan.Block {
		plan.Mutex, plan.Block = true, true
	}
	tg, err := capture.Prepare(e.ctx, proc.Default, pid)
	if err != nil {
		return e.fail(err)
	}
	fmt.Fprint(e.stderr, renderTarget(tg).plain())
	cfg := capture.Config{Label: *label, Interval: *interval, Baseline: *baseline, Duration: *dur, Cooldown: *cooldown,
		Proxy: !*noProxy && len(tg.Process.Ports) > 0, ProxyListen: *proxyListen, Target: *target, Collector: !*noColl,
		OTLP: *otlp != "", OTLPListen: *otlp, OTLPAnyProcess: *otlpAny, Endpoint: *endpoint, Profile: plan, Repo: root}
	if !plan.Empty() {
		if cfg.Profile.Endpoint = tg.EndpointFor(!*noColl); cfg.Profile.Endpoint == nil {
			return e.fail(errors.New("profiling pedido, mas o processo não serve net/http/pprof nem o collector do GitView: \"No profiling data available\""))
		}
		if err := cfg.Profile.Validate(); err != nil {
			return e.fail(err)
		}
		ie := &inspectEnv{stdout: e.stdout, stderr: e.stderr, stdin: e.stdin, yes: e.yes, nonInter: e.nonInter}
		if err := ie.confirm("O profiling fará estas requisições ao processo "+strconv.Itoa(pid)+":", renderCapturePlan(tg, cfg)...); err != nil {
			return e.fail(err)
		}
	} else {
		fmt.Fprintln(e.stderr)
		for _, l := range renderCapturePlan(tg, cfg) {
			fmt.Fprintln(e.stderr, "  "+l)
		}
	}
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	s, err := capture.Start(ctx, st, tg, cfg)
	if err != nil {
		return e.fail(err)
	}
	live := s.Live(1)
	fmt.Fprintln(e.stderr)
	if live.Proxy != "" {
		fmt.Fprintf(e.stderr, "Envie as requisições para  http://%s  (proxy → %s)\n", live.Proxy, live.Target)
	}
	if live.Collector != "" {
		fmt.Fprintln(e.stderr, "O collector registra também as requisições enviadas diretamente à aplicação.")
	}
	if live.OTLP != "" {
		fmt.Fprintf(e.stderr, "Exporte spans OTLP/HTTP para  http://%s\n", live.OTLP)
	}
	if *dur > 0 {
		fmt.Fprintf(e.stderr, "Fase ativa de %s depois de %s de linha de base. Ctrl+C encerra antes.\n", *dur, cfg.Baseline)
	} else {
		fmt.Fprintln(e.stderr, "Enter ou Ctrl+C encerra a fase ativa; Ctrl+C duas vezes cancela sem salvar.")
	}
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	interrupts := 0
	if isTerminal() && *dur == 0 {
		go func() {
			bufio.NewReader(e.stdin).ReadString('\n')
			s.Stop()
		}()
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	interactive := isTerminal()
loop:
	for {
		select {
		case <-s.Done():
			break loop
		case <-sig:
			interrupts++
			if interrupts == 1 {
				s.Stop()
				fmt.Fprintln(e.stderr, "\nencerrando… (Ctrl+C de novo cancela sem salvar)")
			} else {
				s.Cancel()
			}
		case <-tick.C:
			l := s.Live(1)
			line := fmt.Sprintf("%s · %s · CPU %.1f%% · RSS %s · %d requisições", phasePT(l.Phase), l.Elapsed.Round(time.Second), maxf(l.Last.CPUPct, 0), fmtBytes(float64(l.Last.RSS)), l.Requests)
			if interactive {
				fmt.Fprintf(e.stderr, "\r\x1b[2K%s", line)
			} else if int(l.Elapsed.Seconds())%5 == 0 {
				fmt.Fprintln(e.stderr, line)
			}
		}
	}
	if interactive {
		fmt.Fprintln(e.stderr)
	}
	c, err := s.Wait()
	if err != nil {
		if strings.Contains(err.Error(), "cancelled") {
			fmt.Fprintln(e.stderr, "Captura cancelada: nada foi salvo.")
			return 130
		}
		return e.fail(err)
	}
	fmt.Fprintf(e.stderr, "Captura %s salva em %s\n", c.ID, flow.DisplayPath(c.Dir))
	return e.render(c, root)
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// render analyses a capture and prints (and optionally exports) it.
func (e *runtimeEnv) render(c *capture.Capture, root string) int {
	a, err := report.Analyze(c, e.mapper(root, c.Process.MainModule))
	if err != nil {
		return e.fail(err)
	}
	if e.out != "" {
		if err := writeRuntimeHTML(e.out, a, nil); err != nil {
			return e.fail(err)
		}
		fmt.Fprintln(e.stderr, "relatório HTML:", e.out)
	}
	if e.jsonOut {
		return e.printJSON(a)
	}
	fmt.Fprint(e.stdout, renderAnalysis(a, "", 100).plain())
	return exitOK
}

func writeRuntimeHTML(path string, a *report.Analysis, cmp *report.Comparison) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := export.RuntimeHTML(f, a, cmp); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (e *runtimeEnv) history(args []string) int {
	fs := e.flags("history", "uso: gitview runtime history [--json]\n")
	if _, err := parseFlags(fs, args); err != nil {
		return exitUsage
	}
	st, err := e.store(e.repoRoot())
	if err != nil {
		return e.fail(err)
	}
	list, err := st.List()
	if err != nil {
		return e.fail(err)
	}
	if e.jsonOut {
		if list == nil {
			list = []capture.Entry{}
		}
		return e.printJSON(list)
	}
	fmt.Fprint(e.stdout, renderHistory(list).plain())
	return exitOK
}

func (e *runtimeEnv) show(args []string) int {
	fs := e.flags("show", "uso: gitview runtime show <id> [-o relatorio.html] [--json]\n")
	fs.StringVar(&e.out, "o", "", "grava o relatório HTML neste arquivo")
	fs.BoolVar(&e.noSource, "no-source", false, "não relaciona as medições ao código")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(pos) != 1 {
		return e.fail(errors.New("informe o ID da captura (veja gitview runtime history)"))
	}
	root := e.repoRoot()
	st, err := e.store(root)
	if err != nil {
		return e.fail(err)
	}
	c, err := st.Load(pos[0])
	if err != nil {
		return e.fail(err)
	}
	return e.render(c, root)
}

func (e *runtimeEnv) compare(args []string) int {
	fs := e.flags("compare", "uso: gitview runtime compare <idA> <idB> [-o comparacao.html] [--json]\n\nA é a captura de referência (antes), B a nova (depois).\n\n")
	fs.StringVar(&e.out, "o", "", "grava a comparação em HTML neste arquivo")
	fs.BoolVar(&e.noSource, "no-source", false, "não relaciona as medições ao código")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(pos) != 2 {
		return e.fail(errors.New("informe duas capturas: A (antes) e B (depois)"))
	}
	root := e.repoRoot()
	st, err := e.store(root)
	if err != nil {
		return e.fail(err)
	}
	var cs [2]*capture.Capture
	for i, id := range pos {
		if cs[i], err = st.Load(id); err != nil {
			return e.fail(err)
		}
	}
	m := e.mapper(root, cs[0].Process.MainModule)
	var as [2]*report.Analysis
	for i, c := range cs {
		if as[i], err = report.Analyze(c, m); err != nil {
			return e.fail(err)
		}
	}
	cmp := report.Compare(as[0], as[1])
	if e.out != "" {
		if err := writeRuntimeHTML(e.out, as[1], cmp); err != nil {
			return e.fail(err)
		}
		fmt.Fprintln(e.stderr, "comparação HTML:", e.out)
	}
	if e.jsonOut {
		return e.printJSON(cmp)
	}
	fmt.Fprint(e.stdout, renderComparison(cmp).plain())
	return exitOK
}

func (e *runtimeEnv) delete(args []string) int {
	fs := e.flags("delete", "uso: gitview runtime delete <id>\n")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(pos) != 1 {
		return e.fail(errors.New("informe o ID da captura"))
	}
	st, err := e.store(e.repoRoot())
	if err != nil {
		return e.fail(err)
	}
	if err := st.Delete(pos[0]); err != nil {
		return e.fail(err)
	}
	fmt.Fprintln(e.stderr, "captura removida")
	return exitOK
}
