package testrunner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

var ErrBusy = errors.New("já existe uma execução de testes em andamento")

const (
	maxLogLines    = 20000
	maxOutputLines = 400
	waitDelay      = 3 * time.Second
)

// Runner executes one target at a time and exposes snapshots of its state.
// Commands are built as argument vectors and never passed through a shell.
type Runner struct {
	Timeout time.Duration

	mu       sync.Mutex
	state    RunState
	index    map[string]int // project\x00package\x00test -> index in state.Tests
	cancel   context.CancelFunc
	userStop bool
	running  bool
	done     chan struct{}
	notify   chan struct{}
}

func NewRunner(timeout time.Duration) *Runner {
	return &Runner{Timeout: timeout, notify: make(chan struct{}, 1)}
}

// Updates signals (coalesced) whenever the run state changes.
func (r *Runner) Updates() <-chan struct{} { return r.notify }

func (r *Runner) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// Snapshot returns a deep copy of the current state.
func (r *Runner) Snapshot() RunState {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.state
	s.Tests = make([]TestResult, len(r.state.Tests))
	for i, t := range r.state.Tests {
		t.Output = append([]string(nil), t.Output...)
		s.Tests[i] = t
	}
	s.Log = append([]string(nil), r.state.Log...)
	s.Commands = append([]Command(nil), r.state.Commands...)
	return s
}

// Start launches the target in the background. It refuses to start a second
// run while one is in progress.
func (r *Runner) Start(t Target) error {
	if len(t.Units) == 0 {
		return errors.New("nada para executar")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return ErrBusy
	}
	specs := make([]spec, 0, len(t.Units))
	for _, u := range t.Units {
		s, err := buildSpec(u)
		if err != nil {
			for _, s := range specs {
				removeReport(s)
			}
			return err
		}
		specs = append(specs, s)
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	r.cancel, r.userStop, r.running = cancel, false, true
	r.done = make(chan struct{})
	r.index = map[string]int{}
	r.state = RunState{Target: t, Status: Running, Started: time.Now(), Structured: true}
	go r.run(ctx, cancel, specs, timeout, r.done)
	r.signal()
	return nil
}

func removeReport(s spec) {
	if s.report != "" {
		os.Remove(s.report)
	}
}

// Cancel stops the current run, if any.
func (r *Runner) Cancel() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running && r.cancel != nil {
		r.userStop = true
		r.cancel()
	}
}

// Wait blocks until the current run (if any) finishes.
func (r *Runner) Wait() {
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (r *Runner) signal() {
	select {
	case r.notify <- struct{}{}:
	default:
	}
}

func (r *Runner) update(fn func(s *RunState)) {
	r.mu.Lock()
	fn(&r.state)
	r.mu.Unlock()
	r.signal()
}

func (r *Runner) logLine(s *RunState, line string) {
	s.Log = append(s.Log, line)
	if over := len(s.Log) - maxLogLines; over > 0 {
		s.Log = append(s.Log[:0:0], s.Log[over:]...)
	}
}

// result finds or creates the result entry for a test. Callers hold r.mu.
func (r *Runner) result(s *RunState, project, pkg, name string) *TestResult {
	key := project + "\x00" + pkg + "\x00" + name
	if i, ok := r.index[key]; ok {
		return &s.Tests[i]
	}
	r.index[key] = len(s.Tests)
	s.Tests = append(s.Tests, TestResult{Project: project, Package: pkg, Name: name, Status: Running})
	return &s.Tests[len(s.Tests)-1]
}

func appendOutput(t *TestResult, line string) {
	if len(t.Output) < maxOutputLines {
		t.Output = append(t.Output, line)
	}
}

// spec is a fully resolved command for one unit.
type spec struct {
	project *Project
	args    []string
	dir     string
	env     []string
	kind    parserKind
	report  string // node: JSON report file written by the framework
}

type parserKind int

const (
	parseNone parserKind = iota
	parseGoJSON
	parseJestJSON
)

func buildSpec(u Unit) (spec, error) {
	p := u.Project
	if p == nil {
		return spec{}, errors.New("unidade sem projeto")
	}
	s := spec{project: p, dir: p.Dir}
	switch {
	case p.Kind == KindGo && len(p.Command) == 0:
		s.args = []string{"go", "test", "-json", "-count=1"}
		if u.Run != "" {
			s.args = append(s.args, "-run", u.Run)
		}
		pkgs := u.Packages
		if len(pkgs) == 0 {
			pkgs = []string{"./..."}
		}
		s.args = append(s.args, pkgs...)
		s.kind = parseGoJSON
	case p.Kind == KindNode && (p.Framework == "vitest" || p.Framework == "jest"):
		f, err := os.CreateTemp("", "gitview-tests-*.json")
		if err != nil {
			return spec{}, err
		}
		f.Close()
		s.report = f.Name()
		s.args = append([]string{}, p.Bin...)
		if p.Framework == "vitest" {
			if u.Related != "" {
				s.args = append(s.args, "related", u.Related, "--run")
			} else {
				s.args = append(s.args, "run")
			}
			s.args = append(s.args, "--reporter=json", "--outputFile="+s.report)
		} else {
			s.args = append(s.args, "--json", "--outputFile="+s.report, "--ci")
			if u.Related != "" {
				s.args = append(s.args, "--findRelatedTests", u.Related)
			}
		}
		s.args = append(s.args, u.Files...)
		s.env = []string{"CI=1", "FORCE_COLOR=0"}
		s.kind = parseJestJSON
	default:
		if len(p.Command) == 0 {
			return spec{}, fmt.Errorf("sem comando de teste para %s", p.Name)
		}
		s.args = append([]string{}, p.Command...)
		s.env = []string{"CI=1"}
	}
	return s, nil
}

func (r *Runner) run(ctx context.Context, cancel context.CancelFunc, specs []spec, timeout time.Duration, done chan struct{}) {
	defer close(done)
	defer cancel()
	exitCode, infraErr := 0, ""
	structured := true
	for i, s := range specs {
		if ctx.Err() != nil {
			for _, rest := range specs[i:] {
				removeReport(rest)
			}
			break
		}
		code, ok, err := r.exec(ctx, s)
		if s.kind == parseNone || !ok {
			structured = false
		}
		if code != 0 {
			exitCode = code
		}
		if err != nil && infraErr == "" {
			infraErr = err.Error()
		}
		removeReport(s)
	}

	r.mu.Lock()
	defer func() {
		r.running = false
		r.mu.Unlock()
		r.signal()
	}()
	s := &r.state
	s.Finished = time.Now()
	s.ExitCode = exitCode
	s.Structured = structured

	cancelled := ctx.Err() != nil
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) && !r.userStop:
		s.Err = fmt.Sprintf("tempo limite de %s excedido", timeout)
	case cancelled:
		s.Err = "execução cancelada"
	case infraErr != "":
		s.Err = infraErr
	}

	// Tests that never reported a result are not passed: they were cut off.
	leftover := false
	failed := false
	for i := range s.Tests {
		switch s.Tests[i].Status {
		case Running:
			leftover = true
			if cancelled {
				s.Tests[i].Status = Cancelled
			} else {
				s.Tests[i].Status = InfraError
			}
		case Failed:
			failed = true
		}
	}

	switch {
	case cancelled:
		s.Status = Cancelled
	case infraErr != "":
		s.Status = InfraError
	case leftover:
		s.Status = InfraError
		if s.Err == "" {
			s.Err = "o processo terminou antes de reportar todos os resultados"
		}
	case failed:
		s.Status = Failed
	case exitCode == 0:
		s.Status = Passed
	case structured:
		s.Status = InfraError
		s.Err = fmt.Sprintf("o processo terminou com código %d sem falhas reportadas", exitCode)
	default:
		s.Status = Failed
		s.Err = fmt.Sprintf("código de saída %d", exitCode)
	}
}

// exec runs one spec. ok reports whether a structured report was obtained.
func (r *Runner) exec(ctx context.Context, s spec) (code int, ok bool, err error) {
	cmd := exec.CommandContext(ctx, s.args[0], s.args[1:]...)
	cmd.Dir = s.dir
	cmd.Env = append(os.Environ(), s.env...)
	cmd.WaitDelay = waitDelay
	prepareCommand(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return -1, false, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return -1, false, err
	}
	r.update(func(st *RunState) {
		st.Commands = append(st.Commands, Command{Line: quoteArgs(s.args), Dir: s.project.Rel})
		r.logLine(st, fmt.Sprintf("$ %s   (em %s)", quoteArgs(s.args), s.project.Rel))
	})
	if err := cmd.Start(); err != nil {
		return -1, false, fmt.Errorf("não foi possível iniciar %q: %w", s.args[0], err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		r.readLines(stderr, func(st *RunState, line string) { r.logLine(st, line) })
	}()
	sawEvents := false
	go func() {
		defer wg.Done()
		if s.kind == parseGoJSON {
			gp := newGoParser(r, s.project.Name)
			r.readLines(stdout, func(st *RunState, line string) {
				if gp.handle(st, line) {
					sawEvents = true
				}
			})
			return
		}
		r.readLines(stdout, func(st *RunState, line string) { r.logLine(st, line) })
	}()
	wg.Wait()
	waitErr := cmd.Wait()
	killProcessGroup(cmd)

	code = cmd.ProcessState.ExitCode()
	var ee *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &ee) && ctx.Err() == nil {
		err = waitErr
	}

	switch s.kind {
	case parseGoJSON:
		ok = sawEvents
	case parseJestJSON:
		if data, rerr := os.ReadFile(s.report); rerr == nil && len(data) > 0 {
			r.mu.Lock()
			perr := parseJestReport(r, &r.state, s.project, data)
			r.mu.Unlock()
			r.signal()
			ok = perr == nil
		}
	}
	return code, ok, err
}

// readLines feeds each line to fn under the state lock and coalesces UI
// notifications, so a chatty process cannot flood the interface.
func (r *Runner) readLines(rd io.Reader, fn func(st *RunState, line string)) {
	br := bufio.NewReaderSize(rd, 64*1024)
	last := time.Now()
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			line = trimNewline(line)
			r.mu.Lock()
			fn(&r.state, line)
			r.mu.Unlock()
			if time.Since(last) > 50*time.Millisecond {
				r.signal()
				last = time.Now()
			}
		}
		if err != nil {
			r.signal()
			return
		}
	}
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func quoteArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		if a == "" || containsAny(a, " \t\"'$`\\|&;<>()*?") {
			out += fmt.Sprintf("%q", a)
		} else {
			out += a
		}
	}
	return out
}

func containsAny(s, chars string) bool {
	for _, c := range chars {
		for _, d := range s {
			if c == d {
				return true
			}
		}
	}
	return false
}
