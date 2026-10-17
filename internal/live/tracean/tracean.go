// Package tracean is the Trace Analyzer of the Runtime Performance
// Inspector: it relates an execution trace collected during a capture to
// the requests of the capture.
//
// A request is found in the trace only through the task the collector
// creates for it (runtime/trace task "gitview.http" with a
// "gitview.trace_id" log). Without the collector, the trace is placed on
// the capture timeline (by its wall clock when the trace records one) but
// no goroutine is attributed to a request.
package tracean

import (
	"sort"
	"time"

	"github.com/athos/gitview/internal/inspect/xtrace"
)

// Names used by the collector (github.com/athos/gitview/collector).
const (
	TaskName    = "gitview.http"
	LogTraceID  = "gitview.trace_id"
	LogRequest  = "gitview.request"
	maxRegions  = 20
	maxRequests = 5000
)

// RequestTrace is what the trace recorded about one request: the time its
// goroutines spent in each scheduling state within the request's window.
type RequestTrace struct {
	ID         string           `json:"id"`
	Task       uint64           `json:"task"`
	Start      int64            `json:"s"` // ns from the start of the trace
	End        int64            `json:"e"` // -1 when the request did not end within the trace
	Goroutines []int64          `json:"goroutines"`
	RunningNs  int64            `json:"runningNs"`
	RunnableNs int64            `json:"runnableNs"` // ready but waiting for a P: scheduler latency
	WaitingNs  int64            `json:"waitingNs"`
	SyscallNs  int64            `json:"syscallNs"`
	Waits      map[string]int64 `json:"waits,omitempty"` // waiting time by reason
	GCNs       int64            `json:"gcNs"`            // GC activity overlapping the request
	STWNs      int64            `json:"stwNs"`           // stop-the-world pauses overlapping it
	Regions    []RegionTime     `json:"regions,omitempty"`
	Complete   bool             `json:"complete"` // the whole request is inside the trace
}

// RegionTime is the time spent in a user region (runtime/trace.WithRegion)
// during a request.
type RegionTime struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
	Ns    int64  `json:"ns"`
}

// Analysis is the trace of a capture.
type Analysis struct {
	Requests []RequestTrace `json:"requests,omitempty"`
	// Tasks of type gitview.http without a trace ID log (should not happen
	// with the collector) and requests beyond the limit.
	Unmatched int `json:"unmatched,omitempty"`
	// StartWall places the trace on the capture's timeline: known from
	// the trace's own clock reading (Go 1.25+), else approximated by the
	// time the trace was requested.
	StartWall   time.Time    `json:"startWall"`
	WallFromRun bool         `json:"wallFromRun,omitempty"` // true: StartWall is the request time (approximate)
	Stats       xtrace.Stats `json:"stats"`
	DurationNs  int64        `json:"durationNs"`
	Notes       []string     `json:"notes,omitempty"`
}

// Analyze correlates the trace with requests. requested is the wall time
// GitView asked for the trace, used when the trace has no clock reading.
func Analyze(tl *xtrace.Timeline, requested time.Time) *Analysis {
	a := &Analysis{Stats: tl.Stats, DurationNs: tl.DurationNs, StartWall: tl.StartWall}
	if a.StartWall.IsZero() {
		a.StartWall, a.WallFromRun = requested, true
		a.Notes = append(a.Notes, "The trace has no wall-clock reading (Go older than 1.25): it is placed on the capture timeline at the moment it was requested (approximate, within the request's round trip).")
	}
	ids := map[uint64]string{}
	for _, l := range tl.Logs {
		if l.Category == LogTraceID && l.Task != 0 {
			ids[l.Task] = l.Message
		}
	}
	regionsByG := map[int64][]*xtrace.Region{}
	for _, r := range tl.Regions {
		regionsByG[r.G] = append(regionsByG[r.G], r)
	}
	for _, t := range tl.Tasks {
		if t.Type != TaskName {
			continue
		}
		id := ids[t.ID]
		if id == "" || len(a.Requests) >= maxRequests {
			a.Unmatched++
			continue
		}
		rt := RequestTrace{ID: id, Task: t.ID, Start: t.Start, End: t.End, Goroutines: t.Goroutines, Waits: map[string]int64{}}
		end := t.End
		rt.Complete = t.End >= 0 && t.Start > 0
		if end < 0 {
			end = tl.DurationNs
		}
		for _, gid := range t.Goroutines {
			g := tl.Goroutine(gid)
			if g == nil {
				continue
			}
			for _, s := range g.Segments {
				ov := overlap(s.Start, s.End, t.Start, end)
				if ov <= 0 {
					continue
				}
				switch s.State {
				case xtrace.Running:
					rt.RunningNs += ov
				case xtrace.Runnable:
					rt.RunnableNs += ov
				case xtrace.Waiting:
					rt.WaitingNs += ov
					reason := s.Reason
					if reason == "" {
						reason = "unknown"
					}
					rt.Waits[reason] += ov
				case xtrace.Syscall:
					rt.SyscallNs += ov
				}
			}
			regions := map[string]*RegionTime{}
			for _, r := range regionsByG[gid] {
				if r.Task != t.ID {
					continue
				}
				re := r.End
				if re < 0 {
					re = end
				}
				rg := regions[r.Type]
				if rg == nil {
					rg = &RegionTime{Type: r.Type}
					regions[r.Type] = rg
				}
				rg.Count++
				rg.Ns += overlap(r.Start, re, t.Start, end)
			}
			for _, rg := range regions {
				rt.Regions = append(rt.Regions, *rg)
			}
		}
		sort.Slice(rt.Regions, func(i, j int) bool { return rt.Regions[i].Ns > rt.Regions[j].Ns })
		if len(rt.Regions) > maxRegions {
			rt.Regions = rt.Regions[:maxRegions]
		}
		for _, sp := range tl.GC {
			ov := overlap(sp.Start, sp.End, t.Start, end)
			if ov <= 0 {
				continue
			}
			if len(sp.Name) >= 14 && sp.Name[:14] == "stop-the-world" {
				rt.STWNs += ov
			} else {
				rt.GCNs += ov
			}
		}
		if len(rt.Waits) == 0 {
			rt.Waits = nil
		}
		a.Requests = append(a.Requests, rt)
	}
	if len(a.Requests) == 0 {
		a.Notes = append(a.Notes, "No request task in the trace: requests are found in a trace only when the application uses GitView's collector and the request ran while the trace was being recorded.")
	}
	return a
}

func overlap(s, e, ws, we int64) int64 {
	if s < ws {
		s = ws
	}
	if e > we {
		e = we
	}
	if e < s {
		return 0
	}
	return e - s
}

// ByID indexes the requests.
func (a *Analysis) ByID() map[string]*RequestTrace {
	m := make(map[string]*RequestTrace, len(a.Requests))
	for i := range a.Requests {
		m[a.Requests[i].ID] = &a.Requests[i]
	}
	return m
}
