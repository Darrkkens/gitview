package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/athos/gitview/internal/audit"
	"github.com/athos/gitview/internal/flow"
	gmodel "github.com/athos/gitview/internal/graph/model"
)

const auditUsage = `uso: gitview audit <subcomando> [opções] [diretório]

CodeAudit: auditoria de segurança estática do código Go, com evidências
verificáveis (Code Flow, Data Flow, SQL Explorer e o catálogo HTTP do
Repository Graph). Nada do repositório é executado ou modificado; a IA
(Ollama local) só é usada por explain e explain-path, quando pedida.

subcomandos:
  scan          analisa e grava um snapshot (no cache do GitView, fora do repositório)
  findings      lista os findings do último snapshot, com o estado de revisão
  show ID       detalha um finding: evidências, caminho de dados, rotas, código
  review ID     registra uma decisão: --status <estado> --note "justificativa"
  history ID    histórico de decisões de um finding
  diff          compara dois snapshots (padrão: os dois últimos)
  explain ID    pede ao modelo local (Ollama) uma explicação do finding
  explain-path  explica o caminho entre dois nós (--from ID --to ID)
  export        grava o relatório HTML offline (Security Journey) ou JSON
  serve         abre o Security Journey num servidor local (127.0.0.1), com IA e revisão
  rules         lista as regras
  models        mostra o estado do Ollama e os modelos instalados

estados de revisão: open, investigating, confirmed, false_positive,
accepted_risk, fixed, needs_revalidation (confirmed, false_positive,
accepted_risk e fixed exigem --note). A IA nunca muda um estado.

exemplos:
  gitview audit scan
  gitview audit scan --vulndb ~/vulndb          # base OSV local para dependências
  gitview audit findings --severity high
  gitview audit show finding:injection:3f2a
  gitview audit review finding:injection:3f2a --status confirmed --note "reproduzido em staging"
  gitview audit diff
  gitview audit explain finding:authorization:9c1d --model gemma3:4b
  gitview audit export -o security.html
  gitview audit serve
`

type auditFlags struct {
	module, vulndb, state, output, format string
	rules, disable                        string
	includeTests, jsonOut, noSave         bool
	overwrite                             bool
	severity, category, status            string
	note, statusSet                       string
	base, head                            string
	from, to                              string
	ai                                    aiFlags
	port                                  int
	open                                  bool
}

func runAudit(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stderr, auditUsage)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	sub, args := args[0], args[1:]
	known := map[string]bool{"scan": true, "findings": true, "show": true, "review": true, "history": true, "diff": true,
		"explain": true, "explain-path": true, "export": true, "serve": true, "rules": true, "models": true}
	if !known[sub] {
		fmt.Fprintf(stderr, "gitview audit: subcomando desconhecido %q\n\n%s", sub, auditUsage)
		return 2
	}
	fs := flag.NewFlagSet("audit "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var f auditFlags
	fs.StringVar(&f.state, "state", os.Getenv("GITVIEW_AUDIT_DIR"), "diretório dos snapshots e revisões (padrão: cache do GitView, fora do repositório)")
	switch sub {
	case "scan", "export", "serve":
		fs.StringVar(&f.module, "module", "", "analisa só o módulo Go deste diretório")
		fs.StringVar(&f.vulndb, "vulndb", os.Getenv("GITVIEW_VULNDB"), "diretório de uma cópia local da Go vulnerability database (OSV) para GO-DEP-001")
		fs.StringVar(&f.rules, "rules", "", "só estas regras (IDs separados por vírgula)")
		fs.StringVar(&f.disable, "disable", "", "regras a não executar (IDs separados por vírgula)")
		fs.BoolVar(&f.includeTests, "include-tests", false, "procura segredos também em arquivos _test.go")
	}
	switch sub {
	case "scan":
		fs.BoolVar(&f.jsonOut, "json", false, "imprime o resultado em JSON")
		fs.BoolVar(&f.noSave, "no-save", false, "não grava o snapshot")
	case "findings":
		fs.StringVar(&f.severity, "severity", "", "severidade mínima: critical, high, medium, low ou info")
		fs.StringVar(&f.category, "category", "", "authentication, authorization, injection, sensitive-data, configuration ou dependencies")
		fs.StringVar(&f.status, "status", "", "só findings neste estado de revisão")
		fs.BoolVar(&f.jsonOut, "json", false, "imprime em JSON")
	case "show", "history":
		fs.BoolVar(&f.jsonOut, "json", false, "imprime em JSON")
	case "review":
		fs.StringVar(&f.statusSet, "status", "", "novo estado (obrigatório)")
		fs.StringVar(&f.note, "note", "", "justificativa da decisão")
	case "diff":
		fs.StringVar(&f.base, "base", "", "snapshot ou commit antigo (padrão: o penúltimo)")
		fs.StringVar(&f.head, "head", "", "snapshot ou commit novo (padrão: o último)")
		fs.BoolVar(&f.jsonOut, "json", false, "imprime em JSON")
	case "explain", "explain-path", "models":
		f.ai.register(fs)
		fs.BoolVar(&f.jsonOut, "json", false, "imprime em JSON")
		if sub == "explain-path" {
			fs.StringVar(&f.from, "from", "", "nó de origem (ID do grafo: rota, função ou operação)")
			fs.StringVar(&f.to, "to", "", "nó de destino")
		}
	case "export":
		fs.StringVar(&f.output, "output", "", "arquivo de saída (padrão gitview-security.html; JSON vai para a saída padrão)")
		fs.StringVar(&f.output, "o", "", "atalho para --output")
		fs.StringVar(&f.format, "format", "html", "html ou json")
		fs.BoolVar(&f.overwrite, "overwrite", false, "substitui o arquivo de saída se existir")
		fs.BoolVar(&f.noSave, "rescan", false, "analisa de novo em vez de usar o último snapshot")
	case "serve":
		f.ai.register(fs)
		fs.IntVar(&f.port, "port", 0, "porta local (padrão: uma porta livre)")
		fs.BoolVar(&f.open, "open", true, "abre o navegador")
	}
	fs.Usage = func() { fmt.Fprint(stderr, auditUsage+"\nopções:\n"); fs.PrintDefaults() }
	// Flags may come after the positional argument (ID or directory).
	var pos []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		pos = append(pos, rest[0])
		rest = rest[1:]
	}
	dir := "."
	needsID := sub == "show" || sub == "review" || sub == "history" || sub == "explain"
	if needsID {
		if len(pos) == 0 {
			fmt.Fprintf(stderr, "gitview audit %s: informe o ID do finding\n", sub)
			return 2
		}
		if len(pos) > 1 {
			dir = pos[1]
		}
	} else if len(pos) > 0 {
		dir = pos[0]
	}
	if sub == "rules" {
		return printRules(stdout)
	}
	if sub == "models" {
		return runModels(stdout, stderr, f)
	}
	root, err := repoRoot(dir)
	if err != nil {
		fmt.Fprintln(stderr, "gitview audit:", err)
		return 1
	}
	store, err := audit.StoreFor(root, f.state)
	if err != nil {
		fmt.Fprintln(stderr, "gitview audit:", err)
		return 1
	}
	ctx := context.Background()
	switch sub {
	case "scan":
		a, _, err := auditScan(ctx, dir, f, stderr)
		if err != nil {
			fmt.Fprintln(stderr, "gitview audit scan:", err)
			return 1
		}
		id := ""
		if !f.noSave {
			if id, err = store.Save(a); err != nil {
				fmt.Fprintln(stderr, "gitview audit scan: snapshot:", err)
				return 1
			}
		}
		if f.jsonOut {
			return writeJSONOut(stdout, a)
		}
		reviews, _ := store.Reviews()
		printAuditSummary(stdout, a, reviews)
		if id != "" {
			fmt.Fprintf(stderr, "\nsnapshot %s gravado em %s\n", id, store.Dir)
		}
		return 0
	case "findings":
		a, err := latest(store)
		if err != nil {
			fmt.Fprintln(stderr, "gitview audit findings:", err)
			return 1
		}
		reviews, _ := store.Reviews()
		list := filterFindings(a, reviews, f)
		if f.jsonOut {
			return writeJSONOut(stdout, list)
		}
		printFindings(stdout, list, reviews)
		return 0
	case "show", "history", "review", "explain":
		a, err := latest(store)
		if err != nil {
			fmt.Fprintln(stderr, "gitview audit "+sub+":", err)
			return 1
		}
		fd, err := findFinding(a, pos[0])
		if err != nil && sub != "explain" {
			// A finding that is gone (fixed, say) can still be reviewed:
			// look in the older audits, newest first.
			if older := findInSnapshots(store, pos[0]); older != nil {
				fd, err = older, nil
				fmt.Fprintf(stderr, "(%s não está na última auditoria; usando a mais recente em que aparece)\n", older.ID)
			}
		}
		if err != nil {
			fmt.Fprintln(stderr, "gitview audit "+sub+":", err)
			return 1
		}
		reviews, err := store.Reviews()
		if err != nil {
			fmt.Fprintln(stderr, "gitview audit:", err)
			return 1
		}
		switch sub {
		case "show":
			if f.jsonOut {
				return writeJSONOut(stdout, fd)
			}
			printFinding(stdout, root, a, fd, reviews)
		case "history":
			rec := reviews.Findings[fd.ID]
			if f.jsonOut {
				return writeJSONOut(stdout, rec)
			}
			printHistory(stdout, fd, rec)
		case "review":
			if f.statusSet == "" {
				fmt.Fprintln(stderr, "gitview audit review: --status é obrigatório ("+strings.Join(audit.Statuses, ", ")+")")
				return 2
			}
			rec, err := reviews.Decide(fd, f.statusSet, f.note, a.Commit, time.Now())
			if err != nil {
				fmt.Fprintln(stderr, "gitview audit review:", err)
				return 2
			}
			if err := store.SaveReviews(reviews); err != nil {
				fmt.Fprintln(stderr, "gitview audit review:", err)
				return 1
			}
			last := rec.History[len(rec.History)-1]
			fmt.Fprintf(stdout, "%s: %s → %s\n", fd.ID, last.From, last.To)
		case "explain":
			return runExplain(ctx, stdout, stderr, store, root, a, fd, f)
		}
		return 0
	case "explain-path":
		a, err := latest(store)
		if err != nil {
			fmt.Fprintln(stderr, "gitview audit explain-path:", err)
			return 1
		}
		return runExplainPath(ctx, stdout, stderr, store, root, a, f)
	case "diff":
		return runAuditDiff(stdout, stderr, store, f)
	case "export":
		return runAuditExport(ctx, stdout, stderr, store, dir, f)
	case "serve":
		return runAuditServe(ctx, stdout, stderr, dir, f)
	}
	return 0
}

func repoRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if repo, err := OpenRepo(abs); err == nil {
		return repo.Root, nil
	}
	return abs, nil
}

func latest(store *audit.Store) (*audit.Audit, error) {
	a, _, err := store.Latest()
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, errors.New("nenhuma auditoria gravada: rode gitview audit scan")
	}
	return a, nil
}

func writeJSONOut(w io.Writer, v any) int {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return 1
	}
	return 0
}

func splitIDs(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// gitviewVersion is the module version and VCS revision of this binary.
func gitviewVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "devel"
	}
	v := bi.Main.Version
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 12 {
			v += "+" + s.Value[:12]
		}
	}
	return v
}

// auditScan loads the repository (Code Flow and the Repository Graph with
// its HTTP catalog) and runs the rules.
func auditScan(ctx context.Context, dir string, f auditFlags, progress io.Writer) (*audit.Audit, *gmodel.Graph, error) {
	root, progs, failures, took, err := loadGraphPrograms(ctx, dir, f.module, progress)
	if err != nil {
		return nil, nil, err
	}
	g, err := buildRepoGraph(ctx, root, progs, failures, took, false)
	if err != nil {
		return nil, nil, err
	}
	a, err := auditGraph(ctx, root, progs, g, f)
	return a, g, err
}

func auditGraph(ctx context.Context, root string, progs []*flow.Program, g *gmodel.Graph, f auditFlags) (*audit.Audit, error) {
	commit, dirty := "", false
	if repo, err := OpenRepo(root); err == nil {
		if c := repo.HeadCommit(); c != nil {
			commit = c.Hash
		}
		if out, err := repo.git("status", "--porcelain", "--untracked-files=no"); err == nil && len(strings.TrimSpace(string(out))) > 0 {
			dirty = true
		}
	}
	return audit.Scan(ctx, audit.Input{Programs: progs, Graph: g, Root: root, Commit: commit, Dirty: dirty, Version: gitviewVersion()},
		audit.Options{Rules: splitIDs(f.rules), Disable: splitIDs(f.disable), VulnDB: f.vulndb, IncludeTests: f.includeTests})
}

var sevOrder = map[string]int{audit.SevCritical: 5, audit.SevHigh: 4, audit.SevMedium: 3, audit.SevLow: 2, audit.SevInfo: 1}

func filterFindings(a *audit.Audit, r *audit.Reviews, f auditFlags) []*audit.Finding {
	var out []*audit.Finding
	for _, x := range a.Findings {
		if f.severity != "" && sevOrder[x.Severity] < sevOrder[f.severity] {
			continue
		}
		if f.category != "" && x.Category != f.category {
			continue
		}
		if f.status != "" && r.Status(x.ID) != f.status {
			continue
		}
		out = append(out, x)
	}
	return out
}

func findFinding(a *audit.Audit, ref string) (*audit.Finding, error) {
	if f := a.Finding(ref); f != nil {
		return f, nil
	}
	var match []*audit.Finding
	for _, f := range a.Findings {
		if strings.HasPrefix(f.ID, ref) || strings.HasSuffix(f.ID, ref) {
			match = append(match, f)
		}
	}
	switch len(match) {
	case 1:
		return match[0], nil
	case 0:
		return nil, fmt.Errorf("nenhum finding %q no último snapshot", ref)
	}
	var ids []string
	for _, m := range match {
		ids = append(ids, m.ID)
	}
	return nil, fmt.Errorf("%q é ambíguo: %s", ref, strings.Join(ids, ", "))
}

func findInSnapshots(store *audit.Store, ref string) *audit.Finding {
	list, err := store.Snapshots()
	if err != nil {
		return nil
	}
	for i := len(list) - 2; i >= 0; i-- {
		a, _, err := store.Load(list[i].ID)
		if err != nil {
			continue
		}
		if f, err := findFinding(a, ref); err == nil {
			return f
		}
	}
	return nil
}

func printRules(w io.Writer) int {
	for _, r := range audit.Rules() {
		fmt.Fprintf(w, "%-13s %-15s %-9s %-9s %s\n", r.ID, r.Category, r.Severity, r.Basis, r.Title)
	}
	fmt.Fprintf(w, "\nregras versão %s\n", audit.RulesVersion)
	return 0
}

func printAuditSummary(w io.Writer, a *audit.Audit, r *audit.Reviews) {
	commit := a.Commit
	if len(commit) > 12 {
		commit = commit[:12]
	}
	if a.Dirty {
		commit += " (com alterações não commitadas)"
	}
	fmt.Fprintf(w, "CodeAudit · %s · commit %s · regras %s · %d ms\n", a.Repository, commit, a.RulesVersion, a.DurationMS)
	fmt.Fprintf(w, "análise %s\n", a.Status.Status)
	for _, reason := range firstStrings(a.Status.Reasons, 4) {
		fmt.Fprintln(w, "  ·", reason)
	}
	bySev := map[string]int{}
	byVerdict := map[string]int{}
	for _, f := range a.Findings {
		bySev[f.Severity]++
		byVerdict[f.Verdict]++
	}
	fmt.Fprintf(w, "\n%d findings: %d critical, %d high, %d medium, %d low, %d info\n", len(a.Findings), bySev["critical"], bySev["high"], bySev["medium"], bySev["low"], bySev["info"])
	fmt.Fprintf(w, "  %d vulnerable (evidência suficiente) · %d review (revisão necessária) · %d incomplete (análise incompleta)\n\n", byVerdict[audit.Vulnerable], byVerdict[audit.Review], byVerdict[audit.Incomplete])
	printFindings(w, a.Findings, r)
	if !a.Dependencies.Checked {
		fmt.Fprintln(w, "\n"+a.Dependencies.Note)
	} else if a.Dependencies.Stale {
		fmt.Fprintln(w, "\n"+a.Dependencies.Note)
	}
}

func firstStrings(xs []string, n int) []string {
	if len(xs) > n {
		return append(append([]string(nil), xs[:n]...), fmt.Sprintf("… mais %d", len(xs)-n))
	}
	return xs
}

func printFindings(w io.Writer, list []*audit.Finding, r *audit.Reviews) {
	if len(list) == 0 {
		fmt.Fprintln(w, "nenhum finding")
		return
	}
	for _, f := range list {
		status := r.Status(f.ID)
		if rv := r.Revalidation(f); rv != "" {
			status += " (revalidar: " + rv + ")"
		}
		fmt.Fprintf(w, "%-9s %-10s %-9s %-13s %s:%d\n          %s\n          %s · %s\n", f.Severity, f.Verdict, f.Confidence, f.RuleID, f.File, f.Line, f.Title, f.ID, status)
	}
}

func printFinding(w io.Writer, root string, a *audit.Audit, f *audit.Finding, r *audit.Reviews) {
	fmt.Fprintf(w, "%s\n%s\n\n", f.VerdictLabel, f.ID)
	fmt.Fprintf(w, "regra       %s (%s)\nseveridade  %s · confiança %s · veredito %s · análise %s\nrevisão     %s\nlocal       %s:%d\n", f.RuleID, f.Category, f.Severity, f.Confidence, f.Verdict, f.AnalysisStatus, r.Status(f.ID), f.File, f.Line)
	if f.Symbol != nil {
		fmt.Fprintf(w, "função      %s\n", f.Symbol.Qualified)
	}
	fmt.Fprintf(w, "\n%s\n\nPor quê: %s\n", f.Message, f.Why)
	if len(f.Flow) > 0 {
		fmt.Fprintln(w, "\nCaminho dos dados (Data Flow, estático):")
		for _, st := range f.Flow {
			loc := ""
			if st.File != "" {
				loc = fmt.Sprintf("  %s:%d", st.File, st.Line)
			}
			fmt.Fprintf(w, "  %-14s %s%s\n", st.Role, st.Label, loc)
		}
	}
	for _, rt := range f.Routes {
		fmt.Fprintf(w, "\nRota %s · autenticação %s\n", rt.Key, rt.Auth)
		for _, st := range rt.Path {
			check := ""
			if st.Check != "" {
				check = "  ✓ " + st.Check
			}
			fmt.Fprintf(w, "  %-10s %s%s\n", st.Role, st.Name, check)
		}
	}
	fmt.Fprintln(w, "\nEvidências:")
	for _, e := range f.Evidence {
		loc := ""
		if e.File != "" {
			loc = fmt.Sprintf(" (%s:%d)", e.File, e.Line)
		}
		fmt.Fprintf(w, "  %s %-12s %s%s\n", e.ID, e.Kind, e.Text, loc)
		if e.Code != "" {
			fmt.Fprintf(w, "         %s\n", e.Code)
		}
	}
	if f.File != "" && f.Line > 0 && f.RuleID != "GO-DATA-001" {
		if lines := readLines(filepath.Join(root, f.File), f.Line-3, f.Line+3); len(lines) > 0 {
			fmt.Fprintf(w, "\n%s:\n", f.File)
			for i, l := range lines {
				n := f.Line - 3 + i
				if n < 1 {
					continue
				}
				mark := "  "
				if n == f.Line {
					mark = "> "
				}
				fmt.Fprintf(w, "  %s%4d  %s\n", mark, n, audit.Redact(l))
			}
		}
	}
	for _, c := range f.Checks {
		fmt.Fprintf(w, "\n✓ verificação identificada: %s — %s", c.Kind, c.Label)
	}
	if len(f.Checks) > 0 {
		fmt.Fprintln(w)
	}
	for _, u := range f.Unresolved {
		fmt.Fprintf(w, "? não resolvido: %s\n", u)
	}
	for _, l := range f.Limits {
		fmt.Fprintf(w, "! limite: %s\n", l)
	}
	fmt.Fprintln(w, "\nCorreção:")
	for _, x := range f.Remediation {
		fmt.Fprintln(w, "  -", x)
	}
	fmt.Fprintln(w, "Testes:")
	for _, x := range f.Tests {
		fmt.Fprintln(w, "  -", x)
	}
	if rv := r.Revalidation(f); rv != "" {
		fmt.Fprintln(w, "\nrevalidar:", rv)
	}
}

func readLines(path string, from, to int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	if from < 1 {
		from = 1
	}
	if to > len(lines) {
		to = len(lines)
	}
	if from > to {
		return nil
	}
	return lines[from-1 : to]
}

func printHistory(w io.Writer, f *audit.Finding, rec *audit.ReviewRecord) {
	fmt.Fprintf(w, "%s\n%s\n\n", f.ID, f.Title)
	if rec == nil || len(rec.History) == 0 {
		fmt.Fprintln(w, "nenhuma decisão registrada (estado open)")
		return
	}
	for _, d := range rec.History {
		commit := d.Commit
		if len(commit) > 12 {
			commit = commit[:12]
		}
		fmt.Fprintf(w, "%s  %s → %s  commit %s\n  %s\n", d.Time.Local().Format("2006-01-02 15:04"), d.From, d.To, commit, d.Justification)
	}
}

func runAuditDiff(stdout, stderr io.Writer, store *audit.Store, f auditFlags) int {
	list, err := store.Snapshots()
	if err != nil {
		fmt.Fprintln(stderr, "gitview audit diff:", err)
		return 1
	}
	pick := func(ref string, fromEnd int) (*audit.Audit, string, error) {
		if ref != "" {
			return store.Load(ref)
		}
		if len(list) < fromEnd {
			return nil, "", errors.New("são precisos dois snapshots: rode gitview audit scan de novo depois de mudar o código")
		}
		return store.Load(list[len(list)-fromEnd].ID)
	}
	head, hid, err := pick(f.head, 1)
	if err != nil {
		fmt.Fprintln(stderr, "gitview audit diff:", err)
		return 1
	}
	base, bid, err := pick(f.base, 2)
	if err != nil {
		fmt.Fprintln(stderr, "gitview audit diff:", err)
		return 1
	}
	d := audit.Compare(base, head)
	if f.jsonOut {
		return writeJSONOut(stdout, d)
	}
	short := func(c string) string {
		if len(c) > 12 {
			return c[:12]
		}
		return c
	}
	fmt.Fprintf(stdout, "Security Audit Comparison\n\nPrevious: %s (commit %s, análise %s)\nCurrent:  %s (commit %s, análise %s)\n\n", bid, short(d.BaseCommit), d.BaseStatus, hid, short(d.HeadCommit), d.HeadStatus)
	fmt.Fprintf(stdout, "New findings:        %d\nResolved findings:   %d\nUnchanged findings:  %d\nChanged findings:    %d\nNot confirmed gone:  %d\n", len(d.New), len(d.Resolved), len(d.Unchanged), len(d.Changed), len(d.Unconfirmed))
	section := func(title string, items []audit.DiffItem, detail func(audit.DiffItem) string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(stdout, "\n%s\n", title)
		for _, it := range items {
			fmt.Fprintf(stdout, "  %-9s %-13s %s:%d  %s\n", it.Severity, it.RuleID, it.File, it.Line, it.Title)
			if x := detail(it); x != "" {
				fmt.Fprintf(stdout, "            %s\n", x)
			}
		}
	}
	section("New", d.New, func(audit.DiffItem) string { return "" })
	section("Resolved", d.Resolved, func(audit.DiffItem) string { return "" })
	section("Changed", d.Changed, func(it audit.DiffItem) string { return strings.Join(it.Changes, "; ") })
	section("Not confirmed gone", d.Unconfirmed, func(it audit.DiffItem) string { return it.Reason })
	return 0
}

// sortedKeys returns the keys of a map in order.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
