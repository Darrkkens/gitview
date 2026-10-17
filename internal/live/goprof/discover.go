// Package goprof is the Go Profiler of the Runtime Performance Inspector.
// It collects runtime/pprof profiles, runtime/trace execution traces and
// runtime/metrics readings from a running Go process, through the
// endpoints the process itself serves: net/http/pprof, or GitView's
// collector. It never attaches to the process.
package goprof

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/athos/gitview/internal/live/httpcol"
	"github.com/athos/gitview/internal/live/proc"
)

// Kinds of data the profiler collects.
const (
	CPU       = "cpu"
	Heap      = "heap"
	Goroutine = "goroutine"
	Mutex     = "mutex"
	Block     = "block"
	Trace     = "trace"
)

// Endpoint is a source of profiles served by the process.
type Endpoint struct {
	Kind string `json:"kind"` // "collector" | "pprof"
	Addr string `json:"addr"`
	// Exposed: the listener accepts connections from other machines.
	Exposed bool   `json:"exposed,omitempty"`
	Listen  string `json:"listen,omitempty"`
	coll    *httpcol.Collector
	hc      *http.Client
}

// Label describes the endpoint.
func (e *Endpoint) Label() string {
	if e.Kind == "collector" {
		return "GitView collector at " + e.Addr
	}
	return "net/http/pprof at " + e.Addr + "/debug/pprof/"
}

// ErrConflict means the process is already producing a profile of that
// kind (only one CPU profile or trace can run at a time in a process).
var ErrConflict = errors.New("already in use in the process")

// newClient connects only to loopback addresses and never follows a
// redirect (an application redirecting /debug/pprof/ elsewhere must not
// send GitView off the machine).
func newClient() *http.Client { return httpcol.LoopbackClient(2 * time.Second) }

// Discover finds the profiling endpoints of p: the collector (when found
// by httpcol.FindCollector) and net/http/pprof on the ports p listens on.
// A pprof endpoint counts only when the command line it reports is p's.
func Discover(ctx context.Context, fs proc.FS, p *proc.Process, coll *httpcol.Collector) ([]*Endpoint, []string) {
	var out []*Endpoint
	var notes []string
	if coll != nil {
		out = append(out, &Endpoint{Kind: "collector", Addr: coll.Addr, coll: coll})
	}
	if p.OwnNetNS {
		notes = append(notes, "The process runs in its own network namespace (a container): its ports are not reachable at 127.0.0.1 from here, so net/http/pprof is not looked for.")
		return out, notes
	}
	hc := newClient()
	seen := map[int]bool{}
	for _, pt := range p.Ports {
		if seen[pt.Port] {
			continue
		}
		seen[pt.Port] = true
		if coll != nil && !coll.Mounted && pt.Dial() == coll.Addr {
			continue
		}
		addr := pt.Dial()
		cctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		body, status, err := get(cctx, hc, "http://"+addr+"/debug/pprof/", 1<<20)
		cancel()
		if err != nil || status != http.StatusOK || !strings.Contains(string(body), "profile") || !strings.Contains(string(body), "goroutine") {
			continue
		}
		cctx, cancel = context.WithTimeout(ctx, 1500*time.Millisecond)
		cmd, status, err := get(cctx, hc, "http://"+addr+"/debug/pprof/cmdline", 1<<20)
		cancel()
		if err != nil || status != http.StatusOK {
			notes = append(notes, "pprof at "+addr+": cmdline endpoint unavailable; not used")
			continue
		}
		same, err := fs.SameCmdline(p.PID, cmd)
		if err != nil || !same {
			notes = append(notes, fmt.Sprintf("pprof at %s answers for another process (its command line differs from pid %d); not used", addr, p.PID))
			continue
		}
		e := &Endpoint{Kind: "pprof", Addr: addr, hc: hc}
		if !pt.Loopback {
			e.Exposed, e.Listen = true, pt.String()
			notes = append(notes, fmt.Sprintf("Warning: net/http/pprof listens on %s, reachable from other machines: profiles, goroutine stacks and the command line are readable by anyone who can connect. Bind it to 127.0.0.1.", pt.String()))
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		notes = append(notes, "No profiling endpoint: the process serves neither net/http/pprof nor GitView's collector, so CPU, heap, goroutine, mutex and block profiles and execution traces cannot be collected (process metrics from /proc still are).")
	}
	return out, notes
}

func get(ctx context.Context, hc *http.Client, url string, max int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if int64(len(data)) > max {
		return nil, resp.StatusCode, fmt.Errorf("reply larger than %d MiB", max>>20)
	}
	return data, resp.StatusCode, nil
}

// Fetch collects one profile, of at most max bytes. CPU profiles and
// traces last seconds. heap with gc runs a garbage collection in the
// process first.
func (e *Endpoint) Fetch(ctx context.Context, kind string, seconds int, gc bool, max int64) ([]byte, error) {
	if e.coll != nil {
		name := kind
		if kind == Heap && gc {
			// The collector's heap endpoint takes the same parameter.
			name = "heap?gc=1"
		}
		data, err := e.coll.Profile(ctx, name, seconds, max)
		var se *httpcol.StatusError
		if errors.As(err, &se) && se.Code == http.StatusConflict {
			return nil, fmt.Errorf("%s: %w (%s)", kind, ErrConflict, strings.TrimSpace(se.Msg))
		}
		return data, err
	}
	path := ""
	switch kind {
	case CPU:
		path = fmt.Sprintf("profile?seconds=%d", seconds)
	case Trace:
		path = fmt.Sprintf("trace?seconds=%d", seconds)
	case Heap:
		path = "heap"
		if gc {
			path += "?gc=1"
		}
	case Goroutine, Mutex, Block:
		path = kind
	default:
		return nil, fmt.Errorf("unknown profile %q", kind)
	}
	data, status, err := get(ctx, e.hc, "http://"+e.Addr+"/debug/pprof/"+path, max)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		if strings.Contains(msg, "already in use") || strings.Contains(msg, "already enabled") {
			return nil, fmt.Errorf("%s: %w (%s)", kind, ErrConflict, msg)
		}
		return nil, fmt.Errorf("%s: HTTP %d: %s", kind, status, msg)
	}
	return data, nil
}
