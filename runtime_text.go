package main

// Text rendering of the Runtime Performance Inspector, shared by the CLI
// and the TUI. Interface chrome is in Portuguese; facts from the analysis
// keep their English wording. Every number shows its basis (medido,
// amostrado, estimado) and its scope (processo or requisição).

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/athos/gitview/internal/inspect/perf"
	"github.com/athos/gitview/internal/live/capture"
	"github.com/athos/gitview/internal/live/httpcol"
	"github.com/athos/gitview/internal/live/proc"
	"github.com/athos/gitview/internal/live/report"
	"github.com/athos/gitview/internal/live/srcmap"
)

func fmtBytes(n float64) string {
	abs := math.Abs(n)
	switch {
	case abs >= 1<<30:
		return fmt.Sprintf("%.2f GiB", n/(1<<30))
	case abs >= 1<<20:
		return fmt.Sprintf("%.1f MiB", n/(1<<20))
	case abs >= 1<<10:
		return fmt.Sprintf("%.1f KiB", n/(1<<10))
	}
	return fmt.Sprintf("%.0f B", n)
}

func fmtD(d time.Duration) string { return fmtDur(int64(d)) }

// fmtMetric formats a value with its unit.
func fmtMetric(v float64, unit string) string {
	switch unit {
	case "ns":
		return fmtDur(int64(v))
	case "bytes":
		return fmtBytes(v)
	case "bytes/s":
		return fmtBytes(v) + "/s"
	case "%":
		return fmt.Sprintf("%.1f %%", v)
	case "count":
		if v == math.Trunc(v) {
			return fmt.Sprintf("%.0f", v)
		}
		return fmt.Sprintf("%.2f", v)
	}
	return fmt.Sprintf("%g %s", v, unit)
}

func basisPT(b string) string {
	switch b {
	case report.Measured:
		return "medido"
	case report.Sampled:
		return "amostrado"
	case report.Estimated:
		return "estimado"
	case report.Unavailable:
		return "indisponível"
	}
	return b
}

func basisStyle(b string) string {
	switch b {
	case report.Measured:
		return sGood
	case report.Sampled, report.Estimated:
		return sWarn
	}
	return sMuted
}

func scopePT(s string) string {
	if s == report.ScopeRequest {
		return "requisição"
	}
	return "processo"
}

func phasePT(p string) string {
	switch p {
	case capture.PhaseBaseline:
		return "linha de base"
	case capture.PhaseActive:
		return "captura ativa"
	case capture.PhasePaused:
		return "pausada"
	case capture.PhaseCooldown:
		return "pós-captura"
	case capture.PhaseDone:
		return "concluída"
	}
	return p
}

func statusPTCapture(s string) string {
	switch s {
	case capture.StatusComplete:
		return "completa"
	case capture.StatusExited:
		return "processo encerrado durante a captura"
	case capture.StatusRestarted:
		return "processo reiniciado durante a captura"
	}
	return s
}

// sparkline draws values over width cells.
func sparkline(vals []float64, width int) string {
	if len(vals) == 0 || width <= 0 {
		return ""
	}
	levels := []rune(sym.spark)
	if len(vals) > width {
		// Keep the maximum of each bucket: peaks stay visible.
		out := make([]float64, width)
		for i := range out {
			lo, hi := i*len(vals)/width, (i+1)*len(vals)/width
			m := vals[lo]
			for _, v := range vals[lo:max(hi, lo+1)] {
				m = math.Max(m, v)
			}
			out[i] = m
		}
		vals = out
	}
	top := 0.0
	for _, v := range vals {
		top = math.Max(top, v)
	}
	var b strings.Builder
	for _, v := range vals {
		i := 0
		if top > 0 {
			i = int(math.Round(v / top * float64(len(levels)-1)))
		}
		b.WriteRune(levels[min(max(i, 0), len(levels)-1)])
	}
	return b.String()
}

// --- Processes ----------------------------------------------------------------------

func renderProcList(list []*proc.Process) doc {
	var d doc
	d.add(0, spS("Processos Go", sTitle), spS(fmt.Sprintf("  %d encontrados (executáveis deste usuário com informação de build Go) · CPU méd: desde o início do processo", len(list)), sSubtle))
	if len(list) == 0 {
		d.add(0, spS("Nenhum processo Go deste usuário está em execução. Informe um PID diretamente para inspecionar outro processo.", sMuted))
		return d
	}
	d.add(0, spS(fmt.Sprintf("%-8s %-28s %-10s %8s %9s %5s  %s", "PID", "executável", "Go", "CPU méd", "RSS", "thr", "portas"), sBold))
	for _, p := range list {
		ports := make([]string, 0, len(p.Ports))
		for _, pt := range p.Ports {
			ports = append(ports, pt.String())
		}
		name := p.Comm
		if p.Exe != "" {
			name = shortPath(p.Exe, 28)
		}
		d.item(0, fmt.Sprintf("pid:%d", p.PID), "", spS(fmt.Sprintf("%-8d ", p.PID), sAccent), sp(fmt.Sprintf("%-28s ", name)),
			spS(fmt.Sprintf("%-10s %7.1f%% %9s %5d  ", strings.TrimPrefix(p.GoVersion, "go"), p.CPUAvg, fmtBytes(float64(p.RSS)), p.Threads), sSubtle), sp(strings.Join(ports, " ")))
		if p.MainModule != "" {
			d.add(1, spS(p.MainModule, sMuted))
		}
	}
	return d
}

func shortPath(p string, n int) string {
	if len([]rune(p)) <= n {
		return p
	}
	r := []rune(p)
	return "…" + string(r[len(r)-n+1:])
}

func renderTarget(t *capture.Target) doc {
	var d doc
	p := t.Process
	d.add(0, spS("Processo", sTitle), spS(fmt.Sprintf("  PID %d", p.PID), sAccent), spS("  "+p.Comm, sSubtle))
	if p.Exe != "" {
		ex := p.Exe
		if p.ExeDeleted {
			ex += " (o binário foi substituído depois que o processo iniciou)"
		}
		d.add(1, spS("executável  ", sMuted), sp(ex))
	}
	if p.Go {
		info := p.GoVersion
		if p.MainModule != "" {
			info += " · " + p.MainModule
		}
		if p.VCSRevision != "" {
			rev := p.VCSRevision
			if len(rev) > 12 {
				rev = rev[:12]
			}
			info += " · revisão " + rev
			if p.VCSModified {
				info += " (com alterações locais)"
			}
		}
		d.add(1, spS("Go          ", sMuted), sp(info))
	}
	if !p.Started.IsZero() {
		d.add(1, spS("iniciado    ", sMuted), sp(p.Started.Format("2006-01-02 15:04:05")), spS(fmt.Sprintf("  (há %s)", time.Since(p.Started).Round(time.Second)), sSubtle))
	}
	ports := []string{}
	for _, pt := range p.Ports {
		s := pt.String()
		if !pt.Loopback {
			s += " (aceita conexões externas)"
		}
		ports = append(ports, s)
	}
	if len(ports) == 0 {
		ports = []string{"nenhuma porta TCP em escuta"}
	}
	d.add(1, spS("portas      ", sMuted), sp(strings.Join(ports, ", ")))
	d.add(1, spS("recursos    ", sMuted), sp(fmt.Sprintf("RSS %s · %d threads · CPU %s desde o início (%.1f%% de um núcleo em média)", fmtBytes(float64(p.RSS)), p.Threads, p.CPUTime.Round(10*time.Millisecond), p.CPUAvg)))
	d.blank()
	d.add(0, spS("Fontes de dados", sBold))
	d.add(1, spS("✓ ", sGood), sp("/proc: CPU, memória, threads, trocas de contexto"+ioPart(p)))
	if t.Collector != nil {
		where := "listener próprio " + t.Collector.Addr
		if t.Collector.Mounted {
			where = "montado no mux da aplicação (" + t.Collector.Addr + ")"
		}
		d.add(1, spS("✓ ", sGood), sp("collector do GitView: "+where+" — rota, SQL, CPU por requisição, trace por requisição"))
	} else {
		d.add(1, spS("– ", sMuted), spS("collector do GitView não encontrado (opcional)", sMuted))
	}
	for _, e := range t.Endpoints {
		if e.Kind == "pprof" {
			d.add(1, spS("✓ ", sGood), sp("net/http/pprof em "+e.Addr+" — perfis e execution trace"))
		}
	}
	if len(t.Endpoints) == 0 {
		d.add(1, spS("– ", sMuted), spS("nenhum endpoint de profiling (net/http/pprof ou collector)", sMuted))
	}
	d.add(1, spS("✓ ", sGood), sp("proxy local do GitView: correlaciona requisições sem alterar a aplicação"))
	if len(p.Access.Reasons) > 0 {
		d.blank()
		d.add(0, spS("Indisponível", sBold))
		keys := make([]string, 0, len(p.Access.Reasons))
		for k := range p.Access.Reasons {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			d.add(1, spS(k+": ", sMuted), spS(p.Access.Reasons[k], sSubtle))
		}
	}
	if len(t.Notes) > 0 {
		d.blank()
		for _, n := range t.Notes {
			st := sMuted
			if strings.HasPrefix(n, "Warning") {
				st = sWarn
			}
			d.add(0, spS(n, st))
		}
	}
	return d
}

func ioPart(p *proc.Process) string {
	s := ""
	if p.Access.IO {
		s += ", E/S"
	}
	if p.Access.FD {
		s += ", descritores"
	}
	return s
}

// renderCapturePlan describes what a capture will do, for consent.
func renderCapturePlan(t *capture.Target, cfg capture.Config) []string {
	var lines []string
	if cfg.Proxy {
		lines = append(lines, fmt.Sprintf("proxy local em 127.0.0.1 → %s (envie as requisições ao endereço do proxy)", cfg.Target))
	}
	if cfg.Collector && t.Collector != nil {
		lines = append(lines, "arma o collector do GitView na aplicação (POST /debug/gitview/arm): pprof labels e tasks de trace por requisição; desarmado ao final")
	}
	if cfg.OTLP {
		lines = append(lines, "receptor OTLP/HTTP em "+orDefault(cfg.OTLPListen, httpcol.DefaultOTLPAddr))
	}
	pl := cfg.Profile
	if pl.Endpoint == nil {
		pl.Endpoint = t.Endpoint()
	}
	pl.Defaults()
	for _, a := range pl.Actions() {
		lines = append(lines, fmt.Sprintf("%s — %s; %s", a.What, a.Request, a.Effect))
	}
	for _, w := range pl.Warnings() {
		lines = append(lines, "aviso: "+w)
	}
	return lines
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// --- Live ----------------------------------------------------------------------------

func renderLive(l capture.Live, width int) doc {
	var d doc
	st := sAccent
	if l.Phase == capture.PhasePaused {
		st = sWarn
	}
	d.add(0, spS("● ", sBad), spS("Captura "+phasePT(l.Phase), st), spS(fmt.Sprintf("  %s decorridos · ativa há %s · %s", l.Elapsed.Round(100*time.Millisecond), l.Active.Round(100*time.Millisecond), l.ID), sSubtle))
	if l.Exited != "" {
		d.add(0, spS("O processo terminou: "+l.Exited, sBad))
	}
	if l.Proxy != "" {
		d.add(1, spS("proxy  ", sMuted), spS("http://"+l.Proxy, sBold), spS("  → aplicação "+l.Target, sSubtle))
	}
	if l.Collector != "" {
		d.add(1, spS("collector  ", sMuted), sp(l.Collector))
	}
	if l.OTLP != "" {
		d.add(1, spS("OTLP  ", sMuted), sp("http://"+l.OTLP))
	}
	s := l.Last
	if s.HasCPU {
		cpu := []float64{}
		rss := []float64{}
		for _, x := range l.Samples {
			cpu = append(cpu, math.Max(x.CPUPct, 0))
			rss = append(rss, float64(x.RSS))
		}
		w := max(min(width-40, 60), 10)
		d.add(1, spS(fmt.Sprintf("CPU %6.1f %%  ", math.Max(s.CPUPct, 0)), sBold), spS(sparkline(cpu, w), sAccent))
		d.add(1, spS(fmt.Sprintf("RSS %9s  ", fmtBytes(float64(s.RSS))), sBold), spS(sparkline(rss, w), sAccent))
		d.add(1, spS(fmt.Sprintf("threads %d · trocas de contexto %d/%d", s.Threads, s.VolCtx, s.InvolCtx), sSubtle))
	}
	d.blank()
	d.add(0, spS(fmt.Sprintf("Requisições detectadas: %d", l.Requests), sBold))
	for i := len(l.Recent) - 1; i >= 0; i-- {
		r := l.Recent[i]
		d.add(1, spS(fmt.Sprintf("%7.2fs ", r.Off.Seconds()), sSubtle), spS(fmt.Sprintf("%3d ", r.Status), httpStatusStyle(r.Status)),
			sp(fmt.Sprintf("%-6s %s", r.Method, r.Path)), spS("  "+fmtD(r.Dur()), sAccent), spS("  "+sourcesPT(r.Sources), sMuted))
	}
	if l.Requests == 0 {
		d.add(1, spS("Nenhuma ainda. Envie requisições (Postman, curl) ao proxy ou à aplicação instrumentada.", sMuted))
	}
	return d
}

func httpStatusStyle(code int) string {
	switch {
	case code >= 500 || code == 0:
		return sBad
	case code >= 400:
		return sWarn
	}
	return sGood
}

func sourcesPT(src []httpcol.Source) string {
	var s []string
	for _, x := range src {
		s = append(s, string(x))
	}
	return strings.Join(s, "+")
}

// --- Report --------------------------------------------------------------------------

func metricLine(d *doc, indent int, m report.Metric) {
	if m.Basis == report.Unavailable {
		d.add(indent, spS(m.Name+": ", sMuted), spS("indisponível", sMuted), spS(" — "+m.Note, sSubtle))
		return
	}
	d.add(indent, sp(m.Name+": "), spS(fmtMetric(m.Value, m.Unit), sBold), spS(fmt.Sprintf("  [%s · %s]", basisPT(m.Basis), scopePT(m.Scope)), basisStyle(m.Basis)))
	if m.Note != "" {
		d.add(indent+1, spS(m.Note, sSubtle))
	}
}

func latencyLine(l report.Latency) string {
	return fmt.Sprintf("n=%d  mín %s  p50 %s  p90 %s  p99 %s  máx %s  média %s", l.N, fmtD(l.Min), fmtD(l.P50), fmtD(l.P90), fmtD(l.P99), fmtD(l.Max), fmtD(l.Mean))
}

// renderAnalysis renders a capture report. route limits the routes shown
// ("" for all).
func renderAnalysis(a *report.Analysis, route string, width int) doc {
	var d doc
	title := a.ID
	if a.Label != "" {
		title = a.Label + " · " + a.ID
	}
	d.add(0, spS("Runtime Performance", sTitle), sp("  "), spS(title, sBold))
	pi := a.ProcessInfo
	d.add(0, spS(fmt.Sprintf("PID %d · %s · %s · %s", pi.PID, orDefault(pi.Exe, pi.Comm), pi.GoVersion, a.Created.Format("2006-01-02 15:04:05")), sSubtle))
	st := sGood
	if a.Status != capture.StatusComplete {
		st = sWarn
	}
	d.add(0, spS("estado: "+statusPTCapture(a.Status), st), spS("  fontes: "+strings.Join(a.Sources, ", "), sSubtle))
	if a.StatusNote != "" {
		d.add(1, spS(a.StatusNote, sWarn))
	}
	ph := a.Phases
	d.add(0, spS(fmt.Sprintf("fases: linha de base 0–%.1fs · ativa %.1f–%.1fs (%s efetivos) · pós-captura até %.1fs · amostragem a cada %s",
		ph.ActiveStart.Seconds(), ph.ActiveStart.Seconds(), ph.ActiveEnd.Seconds(), ph.Active().Round(10*time.Millisecond), ph.End.Seconds(), a.Settings.Interval), sSubtle))
	for _, w := range a.Warnings {
		d.add(0, spS("⚠ "+w, sWarn))
	}

	// Process.
	d.blank()
	d.add(0, spS("Processo", sBold), spS("  métricas de todo o processo, lidas de /proc (medidas)", sMuted))
	d.add(1, spS(fmt.Sprintf("%-14s %9s %9s %10s %10s %10s %8s %10s", "fase", "CPU méd", "CPU máx", "CPU tempo", "RSS máx", "Δ RSS", "threads", "ctx vol/inv"), sSubtle))
	for _, p := range a.Process.Phases {
		cpuAvg, cpuMax, cpuT := "–", "–", "–"
		if p.HasCPU {
			cpuAvg, cpuMax = fmt.Sprintf("%.1f%%", p.CPUAvg), fmt.Sprintf("%.1f%%", p.CPUMax)
			cpuT = fmtDur(p.CPUTime)
		}
		rss, drss := "–", "–"
		if p.HasMem {
			rss = fmtBytes(float64(p.RSSMax))
			drss = fmtBytes(float64(p.RSSEnd) - float64(p.RSSStart))
		}
		ctx := "–"
		if p.HasCtx {
			ctx = fmt.Sprintf("%d/%d", p.VolCtx, p.InvolCtx)
		}
		d.add(1, sp(fmt.Sprintf("%-14s %9s %9s %10s %10s %10s %8d %10s", phasePT(p.Name), cpuAvg, cpuMax, cpuT, rss, drss, p.Threads, ctx)))
	}
	if act := a.Process.Phases[1]; act.HasIO {
		d.add(1, spS(fmt.Sprintf("E/S na fase ativa: disco leitura %s escrita %s · read()/write() incl. sockets %s / %s", fmtBytes(float64(act.Read)), fmtBytes(float64(act.Write)), fmtBytes(float64(act.RChar)), fmtBytes(float64(act.WChar))), sSubtle))
	}
	if len(a.Process.Series) > 1 {
		cpu, rss := []float64{}, []float64{}
		for _, p := range a.Process.Series {
			cpu = append(cpu, p.CPU)
			rss = append(rss, float64(p.RSS))
		}
		w := max(min(width-20, 80), 10)
		d.add(1, spS("CPU  ", sMuted), spS(sparkline(cpu, w), sAccent))
		d.add(1, spS("RSS  ", sMuted), spS(sparkline(rss, w), sAccent))
	}
	d.add(1, spS("resolução da CPU: "+a.Process.CPUResolution, sSubtle))

	// Routes.
	d.blank()
	d.add(0, spS("Requisições por rota", sBold), spS(fmt.Sprintf("  %d requisições, %d rotas", len(a.Requests), len(a.Routes)), sMuted))
	if len(a.Routes) == 0 {
		d.add(1, spS("Nenhuma requisição foi observada.", sMuted))
	}
	for i, r := range a.Routes {
		if route != "" && r.Key != route {
			continue
		}
		renderRoute(&d, i, &r)
	}

	if len(a.Background) > 0 {
		d.blank()
		d.add(0, spS("SQL fora de requisições (trabalho em segundo plano)", sBold))
		renderStatements(&d, a.Background, 1)
	}

	// Profiles.
	if a.Profiles != nil {
		renderProfiles(&d, a)
	}
	if a.Runtime != nil && len(a.Runtime.Metrics) > 0 {
		d.blank()
		d.add(0, spS("Runtime Go", sBold), spS("  runtime/metrics lidos pelo collector", sMuted))
		for _, m := range a.Runtime.Metrics {
			metricLine(&d, 1, m)
		}
	}
	if a.Trace != nil {
		d.blank()
		d.add(0, spS("Execution trace", sBold), spS(fmt.Sprintf("  %s · %d requisições identificadas · %d ciclos de GC · STW %s", fmtDur(a.Trace.DurationNs), len(a.Trace.Requests), a.Trace.Stats.GCCycles, fmtDur(a.Trace.Stats.STWNs)), sSubtle))
		for _, n := range a.Trace.Notes {
			d.add(1, spS(n, sMuted))
		}
	}
	if len(a.Stale) > 0 {
		d.blank()
		d.add(0, spS("Arquivos alterados depois que o processo iniciou (linhas podem não corresponder):", sWarn))
		for _, f := range a.Stale {
			d.add(1, spS(f, sSubtle))
		}
	}
	d.blank()
	d.add(0, spS("Limitações desta captura", sBold))
	for _, l := range a.Limitations {
		d.add(1, spS("· "+l, sMuted))
	}
	for _, n := range a.Notes {
		d.add(1, spS("· "+n, sMuted))
	}
	return d
}

func renderRoute(d *doc, i int, r *report.RouteStats) {
	d.blank()
	head := []tspan{spS(r.Key, sAccent), spS(fmt.Sprintf("  %d na fase ativa", r.Count), sBold)}
	if r.Outside > 0 {
		head = append(head, spS(fmt.Sprintf(" · %d fora dela (ignoradas)", r.Outside), sMuted))
	}
	if r.Errors > 0 {
		head = append(head, spS(fmt.Sprintf(" · %d com erro", r.Errors), sBad))
	}
	head = append(head, spS("  ("+r.KeyBasis+")", sSubtle))
	d.item(0, fmt.Sprintf("route:%d", i), "", head...)
	if r.Route != nil {
		h := "?"
		l := ""
		if r.Route.Handler != nil {
			h = r.Route.Handler.Name
			l = loc(r.Route.Handler.File, r.Route.Handler.Line)
		}
		ref := ""
		if r.Route.Handler != nil {
			ref = "sym:" + r.Route.Handler.Symbol
		}
		d.item(1, ref, l, spS("handler ", sMuted), sp(h), spS(fmt.Sprintf("  [%s: %s; registrada em %s:%d]", r.Route.Conf, r.Route.Basis, r.Route.File, r.Route.Line), sSubtle))
	}
	if r.Count == 0 {
		return
	}
	codes := []string{}
	keys := make([]int, 0, len(r.Status))
	for k := range r.Status {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	for _, k := range keys {
		codes = append(codes, fmt.Sprintf("%d×%d", r.Status[k], k))
	}
	d.add(1, spS("latência ", sMuted), spS(latencyLine(r.Latency), sBold))
	d.add(2, spS(r.LatencyBasis+" [medido]", sSubtle))
	if r.Proxy != nil {
		d.add(1, spS("no proxy ", sMuted), sp(latencyLine(*r.Proxy)))
	}
	if r.TTFB != nil {
		d.add(1, spS("até os cabeçalhos (proxy) ", sMuted), sp(fmt.Sprintf("p50 %s  p90 %s", fmtD(r.TTFB.P50), fmtD(r.TTFB.P90))))
	}
	if r.Count < 100 && r.Count > 0 {
		d.add(2, spS(fmt.Sprintf("com %d requisições, p99 é a mais lenta delas", r.Count), sSubtle))
	}
	d.add(1, spS(fmt.Sprintf("status %s · bytes enviados ao cliente %s/req · recebidos %s/req · concorrência máx %d · isoladas %d", strings.Join(codes, " "), fmtBytes(r.BytesOut), fmtBytes(r.BytesIn), r.MaxInFlight, r.Isolated), sSubtle))
	for _, m := range r.Metrics {
		metricLine(d, 1, m)
	}
	if r.Trace != nil {
		t := r.Trace
		waits := []string{}
		for k, v := range t.Waits {
			waits = append(waits, fmt.Sprintf("%s %s", k, fmtDur(v)))
		}
		sort.Strings(waits)
		d.add(1, spS("trace (média por requisição) ", sMuted), sp(fmt.Sprintf("executando %s · pronta sem P %s · esperando %s · syscall %s · GC simultâneo %s",
			fmtDur(t.RunningNs), fmtDur(t.RunnableNs), fmtDur(t.WaitingNs), fmtDur(t.SyscallNs), fmtDur(t.GCNs))), spS(fmt.Sprintf("  [medido em %d req.]", t.Requests), sGood))
		if len(waits) > 0 {
			d.add(2, spS("esperas: "+strings.Join(waits, ", "), sSubtle))
		}
	}
	if len(r.Statements) > 0 {
		d.add(1, spS(fmt.Sprintf("SQL: %.0f%% do tempo das requisições", r.SQLShare*100), sMuted), spS("  [medido pelo collector/OpenTelemetry]", sGood))
		renderStatements(d, r.Statements, 2)
	} else if !r.SQLObserved {
		d.add(1, spS("SQL: não observado — só o collector do GitView (ou spans de banco do OpenTelemetry) vê as instruções; a contagem é desconhecida, não zero", sMuted))
	}
	if len(r.HotPath) > 0 {
		d.add(1, spS("caminho crítico (amostras de CPU desta rota)", sMuted))
		renderHotPath(d, r.HotPath, 2)
	}
}

// renderHotPath lists the heaviest path; the frames before the first
// module function (net/http, middleware) are collapsed into one line.
func renderHotPath(d *doc, path []srcmap.PathStep, indent int) {
	lead := 0
	for lead < len(path) && path[lead].Symbol == "" {
		lead++
	}
	if lead == len(path) {
		lead = 0
	}
	if lead > 1 {
		d.add(indent, spS(fmt.Sprintf("… %d quadros fora do módulo até %s", lead, shortFunc(path[lead-1].Name)), sSubtle))
	} else {
		lead = 0
	}
	for _, s := range path[lead:] {
		st, ref := sMuted, ""
		if s.Symbol != "" {
			st, ref = sPlain, "sym:"+s.Symbol
		}
		d.item(indent, ref, "", spS(fmt.Sprintf("%5.1f%% ", s.Pct), sSubtle), spS(s.Name, st))
	}
}

func renderStatements(d *doc, sts []report.StatementStats, indent int) {
	for _, s := range sts {
		per := ""
		if s.PerRequest > 0 {
			per = fmt.Sprintf(" · %.2g/req", s.PerRequest)
		}
		d.add(indent, spS(fmt.Sprintf("%d× ", s.Count), sBold), sp(oneLineSQL(s.Query)))
		info := fmt.Sprintf("total %s · média %s · máx %s%s", fmtD(s.Total), fmtD(s.Mean), fmtD(s.Max), per)
		if s.Rows > 0 {
			info += fmt.Sprintf(" · %.1f linhas", s.Rows)
		}
		if s.Errors > 0 {
			info += fmt.Sprintf(" · %d erros", s.Errors)
		}
		d.add(indent+1, spS(info, sSubtle))
		if s.Match != nil {
			ref := ""
			if s.Match.Func.Symbol != "" {
				ref = "sql:" + s.Match.Func.Symbol
			}
			d.item(indent+1, ref, loc(s.Match.Op.File, s.Match.Op.Line), spS("SQL Explorer: ", sMuted), sp(s.Match.Func.Name), spS(fmt.Sprintf("  [%s: %s]", s.Match.Conf, s.Match.Basis), sSubtle))
		} else if s.Site != "" {
			d.add(indent+1, spS("chamada em "+s.Site+" (sem correspondência no código analisado)", sMuted))
		} else {
			d.add(indent+1, spS("SQL query not statically resolved", sMuted))
		}
	}
}

func renderProfiles(d *doc, a *report.Analysis) {
	p := a.Profiles
	d.blank()
	d.add(0, spS("Perfis", sBold), spS("  amostrados: valores são estimativas estatísticas", sMuted))
	if p.CPU != nil {
		d.add(1, spS("CPU", sAccent), spS(fmt.Sprintf("  total %s · %d amostras · cobertura da fase ativa %.0f%%", p.CPU.FormatValue(p.CPU.Total), p.CPU.Samples, p.CPUCoverage*100), sSubtle))
		if p.CPULabelled > 0 {
			d.add(2, spS(fmt.Sprintf("atribuída a requisições (labels): %s · sem label (outros trabalhos): %s", fmtDur(p.CPULabelled), fmtDur(p.CPUUnlabelled)), sSubtle))
		}
		topProfile(d, p.CPU, 8)
		if len(a.HotPath) > 0 {
			d.add(2, spS("caminho crítico do processo:", sMuted))
			renderHotPath(d, a.HotPath, 3)
		}
		for _, n := range p.CPU.Notes {
			d.add(2, spS(n, sMuted))
		}
	} else {
		d.add(1, spS("No profiling data available", sMuted))
	}
	if p.Allocs != nil {
		d.add(1, spS("Alocações durante a captura", sAccent), spS(fmt.Sprintf("  %s · %d objetos (estimados)", fmtBytes(float64(p.Allocs.Total)), p.Objects), sSubtle))
		topProfile(d, p.Allocs, 5)
	}
	if p.Inuse != nil {
		d.add(1, spS("Heap em uso no fim", sAccent), spS("  "+fmtBytes(float64(p.Inuse.Total)), sSubtle))
	}
	if p.Mutex != nil {
		d.add(1, spS("Contenção de mutex", sAccent), spS("  "+fmtDur(p.Mutex.Total), sSubtle))
		topProfile(d, p.Mutex, 5)
	}
	if p.Block != nil {
		d.add(1, spS("Bloqueios", sAccent), spS("  "+fmtDur(p.Block.Total), sSubtle))
		topProfile(d, p.Block, 5)
	}
	if p.Goroutines != nil {
		d.add(1, spS("Goroutines no fim da fase ativa", sAccent), spS(fmt.Sprintf("  %d", p.Goroutines.Total), sSubtle))
		topProfile(d, p.Goroutines, 5)
	}
	for _, n := range p.Notes {
		d.add(1, spS(n, sMuted))
	}
}

func topProfile(d *doc, rep *perf.Report, n int) {
	// Module functions first (by cumulative value), then the rest.
	var mod, other []perf.FuncStat
	for _, f := range rep.Funcs {
		if f.Flat == 0 && f.Cum == 0 {
			continue
		}
		if f.Symbol != "" {
			mod = append(mod, f)
		} else {
			other = append(other, f)
		}
	}
	sort.SliceStable(mod, func(i, j int) bool { return mod[i].Cum > mod[j].Cum })
	for i, f := range append(mod, other...) {
		if i >= n {
			break
		}
		st, ref := sMuted, ""
		if f.Symbol != "" {
			st, ref = sPlain, "sym:"+f.Symbol
		}
		d.item(2, ref, loc(f.File, f.Line), spS(fmt.Sprintf("cum %-10s flat %-10s ", rep.FormatValue(f.Cum), rep.FormatValue(f.Flat)), sSubtle), spS(f.Name, st))
	}
}

// --- Comparison ---------------------------------------------------------------------

func verdictPT(v string) (string, string) {
	switch v {
	case report.Slower:
		return "mais lenta", sBad
	case report.Faster:
		return "mais rápida", sGood
	case report.NoChange:
		return "sem mudança significativa", sPlain
	}
	return "inconclusivo", sWarn
}

func fmtChange(n report.Num) string {
	if !n.Defined() {
		return "n/d"
	}
	return fmt.Sprintf("%+.1f%%", float64(n))
}

func renderComparison(c *report.Comparison) doc {
	var d doc
	d.add(0, spS("Comparação", sTitle), sp("  "), spS(labelOf(c.A)+"  →  "+labelOf(c.B), sBold))
	d.add(0, spS("A = antes, B = depois. Só rotas presentes nas duas capturas são comparadas.", sMuted))
	d.blank()
	d.add(0, spS("Condições", sBold))
	for _, x := range c.Conditions {
		mark, st := "✓ ", sGood
		if !x.OK {
			mark, st = "✗ ", sBad
			if !x.Blocking {
				st = sWarn
			}
		}
		d.add(1, spS(mark, st), sp(x.Name), spS("  "+x.Detail, sSubtle))
	}
	for _, n := range c.Notes {
		d.add(1, spS(n, sWarn))
	}
	d.blank()
	d.add(0, spS("Rotas", sBold))
	for _, r := range c.Routes {
		v, st := verdictPT(r.Verdict)
		p := "n/d"
		if r.P.Defined() {
			p = fmt.Sprintf("%.3g", float64(r.P))
		}
		d.add(1, spS(r.Key, sAccent), spS("  "+v, st), spS(fmt.Sprintf("  mediana %s → %s (%s) · p = %s (Mann–Whitney) · n %d / %d", fmtD(r.A.P50), fmtD(r.B.P50), fmtChange(r.Change), p, r.NA, r.NB), sSubtle))
		d.add(2, spS(fmt.Sprintf("p90 %s → %s · máx %s → %s", fmtD(r.A.P90), fmtD(r.B.P90), fmtD(r.A.Max), fmtD(r.B.Max)), sSubtle))
		for _, why := range r.Reasons {
			d.add(2, spS("· "+why, sWarn))
		}
		for _, x := range r.Deltas {
			d.add(2, sp(x.Name+": "), spS(fmtMetric(x.A, x.Unit)+" → "+fmtMetric(x.B, x.Unit), sBold), spS(fmt.Sprintf(" (%s)  [%s · %s]", fmtChange(x.Change), basisPT(x.Basis), scopePT(x.Scope)), basisStyle(x.Basis)))
		}
		for _, q := range r.SQLAdded {
			d.add(2, spS("+ SQL novo: ", sWarn), sp(oneLineSQL(q)))
		}
		for _, q := range r.SQLGone {
			d.add(2, spS("− SQL removido: ", sMuted), sp(oneLineSQL(q)))
		}
	}
	if len(c.Routes) == 0 {
		d.add(1, spS("Nenhuma rota em comum.", sMuted))
	}
	if len(c.OnlyA) > 0 {
		d.add(1, spS("só em A: "+strings.Join(c.OnlyA, ", "), sMuted))
	}
	if len(c.OnlyB) > 0 {
		d.add(1, spS("só em B: "+strings.Join(c.OnlyB, ", "), sMuted))
	}
	if len(c.Process) > 0 {
		d.blank()
		d.add(0, spS("Processo", sBold))
		for _, x := range c.Process {
			d.add(1, sp(x.Name+": "), spS(fmtMetric(x.A, x.Unit)+" → "+fmtMetric(x.B, x.Unit), sBold), spS(fmt.Sprintf(" (%s)  [%s · %s]", fmtChange(x.Change), basisPT(x.Basis), scopePT(x.Scope)), basisStyle(x.Basis)))
		}
	}
	if len(c.Functions) > 0 {
		d.blank()
		d.add(0, spS("Funções do módulo com maior mudança de participação na CPU (amostrado)", sBold))
		for _, f := range c.Functions {
			ref := ""
			if f.Symbol != "" {
				ref = "sym:" + f.Symbol
			}
			d.item(1, ref, "", spS(fmt.Sprintf("%5.1f%% → %5.1f%%  ", f.A, f.B), sSubtle), sp(f.Name))
		}
	}
	d.blank()
	if len(c.Regressions) > 0 {
		d.add(0, spS("Possíveis regressões", sBad))
		for _, r := range c.Regressions {
			d.add(1, spS(r, sBad))
		}
	} else {
		d.add(0, spS("Nenhuma regressão estatisticamente significativa.", sMuted))
	}
	return d
}

func labelOf(a *report.Analysis) string {
	if a.Label != "" {
		return a.Label + " (" + a.ID + ")"
	}
	return a.ID
}

func renderHistory(list []capture.Entry) doc {
	var d doc
	d.add(0, spS("Histórico de capturas", sTitle), spS(fmt.Sprintf("  %d", len(list)), sSubtle))
	if len(list) == 0 {
		d.add(0, spS("Nenhuma captura salva para este repositório.", sMuted))
	}
	for _, e := range list {
		label := e.Label
		if label == "" {
			label = filepathBase(e.Exe)
		}
		d.item(0, "cap:"+e.ID, "", spS(e.ID, sAccent), sp("  "+label), spS(fmt.Sprintf("  %s · pid %d · ativa %s · %d req. · %s · %s", e.Created.Format("2006-01-02 15:04"), e.PID, e.Active.Round(100*time.Millisecond), e.Requests, fmtBytes(float64(e.Bytes)), statusPTCapture(e.Status)), sSubtle))
		if len(e.Routes) > 0 {
			d.add(1, spS(strings.Join(first(e.Routes, 4), ", "), sMuted))
		}
	}
	return d
}

func first(s []string, n int) []string {
	if len(s) > n {
		return append(s[:n:n], fmt.Sprintf("+%d", len(s)-n))
	}
	return s
}

func filepathBase(p string) string {
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		return p[i+1:]
	}
	return p
}
