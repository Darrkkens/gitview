package run

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunCapturesExitCodeAndOutput(t *testing.T) {
	r := Run(context.Background(), Spec{Args: []string{"sh", "-c", "echo out; echo err >&2; exit 3"}})
	if r.ExitCode != 3 || strings.TrimSpace(string(r.Stdout)) != "out" || strings.TrimSpace(string(r.Stderr)) != "err" || r.Err != nil {
		t.Fatalf("%+v", r)
	}
}

func TestRunTimeoutAndCancelReachChildren(t *testing.T) {
	start := time.Now()
	r := Run(context.Background(), Spec{Args: []string{"sh", "-c", "sleep 30 & wait"}, Timeout: 300 * time.Millisecond})
	if !r.TimedOut || time.Since(start) > 5*time.Second {
		t.Fatalf("timeout: %+v after %s", r, time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	r = Run(ctx, Spec{Args: []string{"sh", "-c", "sleep 30"}})
	if !r.Cancelled {
		t.Fatalf("cancel: %+v", r)
	}
}

func TestCommandLineQuotesAndRootCheck(t *testing.T) {
	s := Spec{Args: []string{"go", "test", "-run", "^TestX$", "./pkg"}}
	if got := s.CommandLine(); got != `go test -run "^TestX$" ./pkg` {
		t.Errorf("command line %s", got)
	}
	if InsideRoot("/repo", "/repo/a/b") != nil || InsideRoot("/repo", "/other") == nil || InsideRoot("/repo", "/repo/../x") == nil {
		t.Error("InsideRoot")
	}
	if Run(context.Background(), Spec{Args: []string{"definitely-not-a-command-gitview"}}).Err == nil {
		t.Error("start failure not reported")
	}
}

func TestKillAllStopsRunningCommands(t *testing.T) {
	done := make(chan Result, 1)
	go func() {
		done <- Run(context.Background(), Spec{Args: []string{"sh", "-c", "sleep 30 & sleep 30; wait"}})
	}()
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	KillAll()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("command still running after KillAll")
	}
	if time.Since(start) > 4*time.Second {
		t.Errorf("KillAll took %s", time.Since(start))
	}
}

func TestEnvironmentHidesTheDSN(t *testing.T) {
	t.Setenv("GITVIEW_PG_DSN", "postgres://u:secret@localhost/db")
	r := Run(context.Background(), Spec{Args: []string{"sh", "-c", "echo ${GITVIEW_PG_DSN:-unset}"}})
	if strings.TrimSpace(string(r.Stdout)) != "unset" {
		t.Errorf("project code sees the DSN: %s", r.Stdout)
	}
}
