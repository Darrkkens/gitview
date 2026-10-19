package main

import (
	"context"
	"flag"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/athos/gitview/internal/audit"
	"github.com/athos/gitview/internal/flow"
)

// webUI is the web interface started from the terminal ('w'): the server of
// `gitview serve` over the same repository, alive as long as the TUI.
type webUI struct {
	url    string
	cancel context.CancelFunc
}

type webStartedMsg struct {
	web *webUI
	err error
}

// openWeb starts the local server once and opens the browser; later
// presses open the page again.
func (m *model) openWeb() tea.Cmd {
	if m.web != nil {
		url := m.web.url
		m.setToast("Interface web em "+url, false)
		return func() tea.Msg {
			_ = startBrowser(url)
			return nil
		}
	}
	m.setToast("Abrindo a interface web…", false)
	root := m.repo.Root
	return func() tea.Msg {
		var f auditFlags
		fs := flag.NewFlagSet("web", flag.ContinueOnError)
		f.ai.register(fs) // the defaults of gitview serve
		f.state = os.Getenv("GITVIEW_AUDIT_DIR")
		f.vulndb = os.Getenv("GITVIEW_VULNDB")
		store, err := audit.StoreFor(root, f.state)
		if err != nil {
			return webStartedMsg{err: err}
		}
		w := &workspace{dir: root, root: root, f: f, store: store, progress: io.Discard, cache: flow.NewCache()}
		w.inv, w.invErr = newInvestigator(store, root, f.ai)
		w.layout, _ = layoutFile(root)
		ctx, cancel := context.WithCancel(context.Background())
		var changes chan struct{}
		if ev, err := watch(root); err == nil {
			changes = make(chan struct{}, 1)
			go w.forward(ctx, ev, changes)
		}
		srv := w.server("", changes)
		ln, err := srv.Listen(0)
		if err != nil {
			cancel()
			return webStartedMsg{err: err}
		}
		go srv.Serve(ctx, ln)
		srv.Warm(ctx)
		_ = startBrowser(srv.URL())
		return webStartedMsg{web: &webUI{url: srv.URL(), cancel: cancel}}
	}
}

func (m *model) webStarted(msg webStartedMsg) {
	if msg.err != nil {
		m.setToast("Interface web: "+msg.err.Error(), true)
		return
	}
	m.web = msg.web
	m.setToast("Interface web em "+m.web.url, false)
}

func (m *model) webStop() {
	if m.web != nil {
		m.web.cancel()
		m.web = nil
	}
}
