package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/athos/gitview/internal/audit"
	"github.com/athos/gitview/internal/audit/ai"
)

// aiFlags configure the local model. They never come from the analysed
// repository's .gitview.json: a repository must not be able to send its
// reader's code somewhere else.
type aiFlags struct {
	model, endpoint string
	allowRemote     bool
	timeout         time.Duration
	maxTokens       int
}

func (a *aiFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&a.model, "model", "", "modelo do Ollama (padrão: GITVIEW_AI_MODEL ou o menor Gemma instalado; nada é baixado)")
	fs.StringVar(&a.endpoint, "ollama", "", "endereço do Ollama (padrão: GITVIEW_OLLAMA_URL, OLLAMA_HOST ou "+ai.DefaultEndpoint+")")
	fs.BoolVar(&a.allowRemote, "ai-allow-remote", false, "permite um endereço fora desta máquina (o código dos trechos é enviado a ele)")
	fs.DurationVar(&a.timeout, "ai-timeout", 10*time.Minute, "tempo limite de uma resposta do modelo (Ctrl+C cancela antes)")
	fs.IntVar(&a.maxTokens, "ai-max-tokens", 6000, "orçamento estimado do contexto enviado")
}

func (a aiFlags) config() ai.Config {
	return ai.Config{Endpoint: a.endpoint, Model: a.model, AllowRemote: a.allowRemote, Timeout: a.timeout, MaxTokens: a.maxTokens}.FromEnv()
}

// repoReader reads repository files for the snippets sent to the model:
// only relative paths that stay inside the root, and no symlinks out of it.
func repoReader(root string) ai.SourceReader {
	realRoot, _ := filepath.EvalSymlinks(root)
	return func(file string) ([]string, error) {
		clean := filepath.Clean(filepath.FromSlash(file))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, errors.New("outside the repository")
		}
		abs := filepath.Join(root, clean)
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, errors.New("not available in the working tree")
		}
		if realRoot != "" && !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
			return nil, errors.New("outside the repository")
		}
		data, err := os.ReadFile(real)
		if err != nil {
			return nil, err
		}
		return strings.Split(string(data), "\n"), nil
	}
}

func newInvestigator(store *audit.Store, root string, f aiFlags) (*ai.Investigator, error) {
	cl, err := ai.NewClient(f.config())
	if err != nil {
		return nil, err
	}
	return &ai.Investigator{Client: cl, Cache: store, Read: repoReader(root)}, nil
}

func runModels(stdout, stderr io.Writer, f auditFlags) int {
	cl, err := ai.NewClient(f.ai.config())
	if err != nil {
		fmt.Fprintln(stderr, "gitview audit models:", err)
		return 1
	}
	st := cl.Probe(context.Background())
	if f.jsonOut {
		return writeJSONOut(stdout, st)
	}
	fmt.Fprintf(stdout, "Ollama %s: ", st.Endpoint)
	if !st.Reachable {
		fmt.Fprintf(stdout, "não respondeu (%s)\nInicie o Ollama (ollama serve); o GitView não o instala nem o inicia.\n", st.Error)
		return 1
	}
	fmt.Fprintln(stdout, "respondendo")
	for _, m := range st.Models {
		mark := "  "
		if m.Name == st.Selected {
			mark = "* "
		}
		fmt.Fprintf(stdout, "%s%-24s %-8s %s\n", mark, m.Name, m.Params, humanBytes(m.Size))
	}
	if st.Selected == "" {
		fmt.Fprintln(stdout, st.Error)
	} else {
		fmt.Fprintf(stdout, "\nmodelo padrão: %s\n", st.Selected)
	}
	return 0
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d B", n)
}

func runExplain(ctx context.Context, stdout, stderr io.Writer, store *audit.Store, root string, a *audit.Audit, fd *audit.Finding, f auditFlags) int {
	iv, err := newInvestigator(store, root, f.ai)
	if err != nil {
		fmt.Fprintln(stderr, "gitview audit explain:", err)
		return 1
	}
	fmt.Fprintf(stderr, "perguntando a %s sobre %s… (Ctrl+C cancela)\n", iv.Client.Endpoint(), fd.ID)
	ctx, stop := signalContext(ctx)
	defer stop()
	res, err := iv.ExplainFinding(ctx, ai.InvestigationRequest{Finding: fd, Model: f.ai.model})
	if err != nil {
		fmt.Fprintln(stderr, "gitview audit explain:", err)
		return 1
	}
	if f.jsonOut {
		return writeJSONOut(stdout, res)
	}
	printInvestigation(stdout, res)
	return 0
}

func runExplainPath(ctx context.Context, stdout, stderr io.Writer, store *audit.Store, root string, a *audit.Audit, f auditFlags) int {
	if f.from == "" || f.to == "" {
		fmt.Fprintln(stderr, "gitview audit explain-path: --from e --to são obrigatórios (IDs de nós do grafo, como os de gitview audit show)")
		return 2
	}
	_, g, err := auditScan(ctx, root, f, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "gitview audit explain-path:", err)
		return 1
	}
	p, err := audit.PathBetween(g, a, f.from, f.to)
	if err != nil {
		fmt.Fprintln(stderr, "gitview audit explain-path:", err)
		return 1
	}
	iv, err := newInvestigator(store, root, f.ai)
	if err != nil {
		fmt.Fprintln(stderr, "gitview audit explain-path:", err)
		return 1
	}
	ctx, stop := signalContext(ctx)
	defer stop()
	res, err := iv.ExplainPath(ctx, pathRequest(p, f.from, f.to, f.ai.model))
	if err != nil {
		fmt.Fprintln(stderr, "gitview audit explain-path:", err)
		return 1
	}
	if f.jsonOut {
		return writeJSONOut(stdout, res)
	}
	printInvestigation(stdout, res)
	return 0
}

func pathRequest(p *audit.GraphPath, from, to, model string) ai.PathInvestigationRequest {
	req := ai.PathInvestigationRequest{From: from, To: to, Related: p.Related, Unresolved: p.Unresolved, Model: model}
	for _, s := range p.Steps {
		req.Steps = append(req.Steps, ai.PathStep{ID: s.ID, Name: s.Name, Role: s.Role, File: s.File, Line: s.Line, Conf: s.Conf, Check: s.Check})
	}
	return req
}

func printInvestigation(w io.Writer, res ai.InvestigationResult) {
	cached := ""
	if res.Cached {
		cached = " · do cache"
	}
	fmt.Fprintf(w, "AI analysis (%s%s) · %s · %d ms\n", res.Model, cached, res.Subject, res.DurationMS)
	fmt.Fprintln(w, "Isto é uma interpretação do modelo, não um finding: as referências abaixo foram conferidas com o contexto enviado.")
	if res.Answer == nil {
		fmt.Fprintln(w, "\nA resposta não é JSON válido; nada foi aceito:")
		fmt.Fprintln(w, res.Raw)
		return
	}
	an := res.Answer
	fmt.Fprintf(w, "\n%s\n", an.Summary)
	if an.Problem != "" {
		fmt.Fprintf(w, "\nProblema possível: %s\n", an.Problem)
	}
	if an.Why != "" {
		fmt.Fprintf(w, "Por quê: %s\n", an.Why)
	}
	fmt.Fprintf(w, "Confiança do modelo: %s\n", an.Confidence)
	list := func(title string, xs []string) {
		if len(xs) == 0 {
			return
		}
		fmt.Fprintf(w, "\n%s:\n", title)
		for _, x := range xs {
			fmt.Fprintln(w, "  -", x)
		}
	}
	list("Caminho dos dados", an.DataPath)
	list("Transformações", an.Transformations)
	list("Validações", an.Validations)
	list("Verificações de segurança", an.SecurityChecks)
	list("Pontos de revisão", an.ReviewPoints)
	list("Relações não determinadas", an.Undetermined)
	list("Informação que falta", an.MissingInformation)
	list("Correção", an.Recommendations)
	list("Testes", an.SuggestedTests)
	fmt.Fprintf(w, "\nEvidências citadas e aceitas: %s\n", strings.Join(res.Validation.AcceptedIDs, ", "))
	for _, r := range an.References {
		fmt.Fprintf(w, "  %s %s:%d-%d %s\n", r.EvidenceID, r.File, r.StartLine, r.EndLine, r.Symbol)
	}
	for _, r := range res.Validation.Rejected {
		fmt.Fprintf(w, "  rejeitada: %s — %s\n", r.Reference, r.Reason)
	}
	for _, x := range res.Validation.Warnings {
		fmt.Fprintf(w, "  aviso: %s\n", x)
	}
	if an.SuggestedStatus != "" {
		fmt.Fprintf(w, "\nSugestão de estado (não aplicada): %s — %s\nPara decidir: gitview audit review %s --status <estado> --note \"…\"\n", an.SuggestedStatus, an.StatusRationale, res.Subject)
	}
	if !res.Validation.Valid {
		fmt.Fprintln(w, "\nA resposta não cita nenhuma evidência válida do contexto: trate-a com cautela.")
	}
}

// envVulnDB is the local vulnerability database configured in the user's
// environment (the TUI has no flags).
func envVulnDB() string { return os.Getenv("GITVIEW_VULNDB") }
