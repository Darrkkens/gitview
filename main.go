// GitView is a live terminal monitor for uncommitted changes in a git repository.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/athos/gitview/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "flow" {
		os.Exit(runFlow(os.Args[2:], os.Stdout, os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "inspect" {
		os.Exit(runInspect(os.Args[2:], os.Stdout, os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "study" {
		os.Exit(runStudy(os.Args[2:], os.Stdout, os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "graph" {
		os.Exit(runGraph(os.Args[2:], os.Stdout, os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "runtime" {
		os.Exit(runRuntime(os.Args[2:], os.Stdout, os.Stderr))
	}
	useDelta := flag.Bool("delta", false, "renderiza diffs com git-delta (alternável com 'd')")
	expandAll := flag.Bool("expand", false, "expande automaticamente arquivos novos")
	interval := flag.Duration("interval", 2*time.Second, "intervalo de polling de segurança")
	noWatch := flag.Bool("no-watch", false, "desativa fsnotify e usa apenas polling")
	exportDir := flag.String("export-dir", os.Getenv("GITVIEW_EXPORT_DIR"), "diretório padrão dos relatórios exportados (x)")
	testTimeout := flag.Duration("test-timeout", 0, "tempo limite de uma execução de testes (padrão 10m)")
	ascii := flag.Bool("ascii", os.Getenv("GITVIEW_ASCII") != "", "usa símbolos ASCII em vez de Unicode")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "uso: gitview [opções] [diretório]\n       gitview flow [opções] [diretório]      (veja gitview flow -h)\n       gitview inspect [subcomando] [opções]  (veja gitview inspect help)\n       gitview runtime <subcomando> [opções]  (veja gitview runtime help)\n       gitview study [-o study.html] [opções]  (relatório com a aba Study)\n       gitview graph export [opções]          (Repository Graph; veja gitview graph help)\n\n")
		flag.PrintDefaults()
		fmt.Fprint(os.Stderr, `
atalhos principais:
  Enter  expandir/recolher     /  busca global      n/N  ocorrências
  t      painel de testes      x  exportar          ?    ajuda contextual

configuração opcional em <repo>/.gitview.json:
  {"exportDir": "reports",
   "tests": {"timeout": "5m",
             "projects": [{"name": "api", "kind": "go", "dir": "backend"},
                          {"name": "e2e", "kind": "command", "dir": "web",
                           "command": ["npm", "run", "test:e2e"]}]}}
`)
	}
	flag.Parse()

	if *ascii {
		sym = asciiSymbols
	}

	dir := "."
	if flag.NArg() > 0 {
		dir = flag.Arg(0)
	}
	repo, err := OpenRepo(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gitview:", err)
		os.Exit(1)
	}

	var events <-chan struct{}
	if !*noWatch {
		events, _ = watch(repo.Root) // on failure fall back to polling only
	}

	cfg, err := config.Load(repo.Root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gitview:", err)
		os.Exit(1)
	}
	opt := options{interval: *interval, useDelta: *useDelta, expandAll: *expandAll,
		exportDir: *exportDir, testTimeout: *testTimeout, testConfig: cfg.Tests.Projects}
	if opt.exportDir == "" && cfg.ExportDir != "" {
		opt.exportDir = cfg.ExportDir
		if !filepath.IsAbs(opt.exportDir) {
			opt.exportDir = filepath.Join(repo.Root, opt.exportDir)
		}
	}
	if opt.testTimeout == 0 {
		if opt.testTimeout, err = cfg.TestTimeout(); err != nil {
			fmt.Fprintln(os.Stderr, "gitview:", err)
			os.Exit(1)
		}
	}
	m := newModel(repo, events, opt)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gitview:", err)
		os.Exit(1)
	}
}
