package tracean

import (
	"bytes"
	"context"
	"runtime/trace"
	"testing"
	"time"

	"github.com/athos/gitview/internal/inspect/xtrace"
)

// request emulates what the collector does around a handler.
func request(id string, work func(ctx context.Context)) {
	ctx, task := trace.NewTask(context.Background(), TaskName)
	trace.Log(ctx, LogTraceID, id)
	work(ctx)
	task.End()
}

func TestRequestsAreFoundInTheTrace(t *testing.T) {
	var buf bytes.Buffer
	if err := trace.Start(&buf); err != nil {
		t.Skip("tracing busy:", err)
	}
	requested := time.Now()
	request("aaaa1111", func(ctx context.Context) {
		trace.WithRegion(ctx, "db.query", func() { time.Sleep(20 * time.Millisecond) })
		ch := make(chan int)
		go func() { time.Sleep(10 * time.Millisecond); ch <- 1 }()
		<-ch
		x := 0
		for i := 0; i < 3e7; i++ {
			x += i
		}
		_ = x
	})
	request("bbbb2222", func(ctx context.Context) {})
	// A task of another type is not a request.
	_, other := trace.NewTask(context.Background(), "batch")
	other.End()
	trace.Stop()

	tl, err := xtrace.Read(&buf, nil, xtrace.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	a := Analyze(tl, requested)
	if len(a.Requests) != 2 || a.Unmatched != 0 {
		t.Fatalf("requests: %+v", a.Requests)
	}
	r := a.ByID()["aaaa1111"]
	if r == nil || !r.Complete || r.End <= r.Start {
		t.Fatalf("request: %+v", r)
	}
	if r.WaitingNs < int64(25*time.Millisecond) || r.RunningNs <= 0 {
		t.Errorf("states: running=%d waiting=%d", r.RunningNs, r.WaitingNs)
	}
	if len(r.Waits) == 0 {
		t.Error("no wait reasons")
	}
	if len(r.Regions) != 1 || r.Regions[0].Type != "db.query" || r.Regions[0].Ns < int64(20*time.Millisecond) {
		t.Errorf("regions: %+v", r.Regions)
	}
	total := r.RunningNs + r.RunnableNs + r.WaitingNs + r.SyscallNs
	if total > (r.End-r.Start)*int64(len(r.Goroutines))+int64(time.Millisecond) {
		t.Errorf("state time %d exceeds the window %d", total, r.End-r.Start)
	}
	// Go 1.25 traces carry their wall clock.
	if a.WallFromRun {
		t.Log("trace without clock snapshot; approximate placement")
	} else if d := a.StartWall.Sub(requested); d < -time.Second || d > time.Second {
		t.Errorf("trace wall clock %v far from %v", a.StartWall, requested)
	}
}

func TestTraceWithoutCollectorTasks(t *testing.T) {
	var buf bytes.Buffer
	if err := trace.Start(&buf); err != nil {
		t.Skip("tracing busy:", err)
	}
	time.Sleep(time.Millisecond)
	trace.Stop()
	tl, err := xtrace.Read(&buf, nil, xtrace.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	a := Analyze(tl, time.Now())
	if len(a.Requests) != 0 || len(a.Notes) == 0 {
		t.Errorf("analysis: %+v", a)
	}
}
