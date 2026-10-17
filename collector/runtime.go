package collector

import (
	"math"
	"runtime/metrics"
	"sync"
	"time"
)

// Counters are process-wide runtime counters read at the start and end of
// a request; their difference covers everything the process did in that
// window, not only the request.
type Counters struct {
	GCCycles uint64 `json:"gcCycles"`
}

var counterSamples = sync.Pool{New: func() any {
	return []metrics.Sample{{Name: "/gc/cycles/total:gc-cycles"}}
}}

func readCounters() Counters {
	s := counterSamples.Get().([]metrics.Sample)
	metrics.Read(s)
	var c Counters
	if s[0].Value.Kind() == metrics.KindUint64 {
		c.GCCycles = s[0].Value.Uint64()
	}
	counterSamples.Put(s)
	return c
}

func (c Counters) sub(o Counters) Counters {
	return Counters{GCCycles: c.GCCycles - o.GCCycles}
}

// runtimeMetrics are the runtime/metrics GitView polls during a capture.
// Reading them does not stop the world.
var runtimeMetrics = []string{
	"/gc/cycles/total:gc-cycles",
	"/gc/heap/allocs:bytes",
	"/gc/heap/allocs:objects",
	"/gc/heap/frees:bytes",
	"/gc/heap/live:bytes",
	"/gc/heap/goal:bytes",
	"/gc/heap/objects:objects",
	"/gc/gogc:percent",
	"/gc/gomemlimit:bytes",
	"/memory/classes/total:bytes",
	"/memory/classes/heap/objects:bytes",
	"/sched/goroutines:goroutines",
	"/sched/gomaxprocs:threads",
	"/sched/latencies:seconds",
	"/sched/pauses/total/gc:seconds",
	"/gc/pauses:seconds",
	"/sync/mutex/wait/total:seconds",
	"/cpu/classes/total:cpu-seconds",
	"/cpu/classes/user:cpu-seconds",
	"/cpu/classes/gc/total:cpu-seconds",
	"/cpu/classes/idle:cpu-seconds",
	"/cpu/classes/scavenge/total:cpu-seconds",
}

var (
	metricsOnce sync.Once
	metricNames []string
)

func availableMetrics() []string {
	metricsOnce.Do(func() {
		have := map[string]bool{}
		for _, d := range metrics.All() {
			have[d.Name] = true
		}
		for _, n := range runtimeMetrics {
			if have[n] {
				metricNames = append(metricNames, n)
			}
		}
		// /gc/pauses:seconds is the older name of the GC pause histogram.
		if have["/sched/pauses/total/gc:seconds"] {
			for i, n := range metricNames {
				if n == "/gc/pauses:seconds" {
					metricNames = append(metricNames[:i], metricNames[i+1:]...)
					break
				}
			}
		}
	})
	return metricNames
}

// Histogram is a runtime/metrics histogram: Counts[i] observations fell in
// [Buckets[i], Buckets[i+1]). Infinite bounds are sent as ±1e308.
type Histogram struct {
	Counts  []uint64  `json:"counts"`
	Buckets []float64 `json:"buckets"`
}

// RuntimeSnapshot is the reply of the runtime endpoint.
type RuntimeSnapshot struct {
	Now        int64                `json:"now"` // Unix nanoseconds
	Uint       map[string]uint64    `json:"uint,omitempty"`
	Float      map[string]float64   `json:"float,omitempty"`
	Histograms map[string]Histogram `json:"histograms,omitempty"`
}

func readRuntime() RuntimeSnapshot {
	names := availableMetrics()
	samples := make([]metrics.Sample, len(names))
	for i, n := range names {
		samples[i].Name = n
	}
	metrics.Read(samples)
	snap := RuntimeSnapshot{Now: time.Now().UnixNano(), Uint: map[string]uint64{}, Float: map[string]float64{}, Histograms: map[string]Histogram{}}
	for _, s := range samples {
		switch s.Value.Kind() {
		case metrics.KindUint64:
			snap.Uint[s.Name] = s.Value.Uint64()
		case metrics.KindFloat64:
			snap.Float[s.Name] = s.Value.Float64()
		case metrics.KindFloat64Histogram:
			h := s.Value.Float64Histogram()
			b := make([]float64, len(h.Buckets))
			for i, v := range h.Buckets {
				switch {
				case math.IsInf(v, 1):
					v = 1e308
				case math.IsInf(v, -1):
					v = -1e308
				}
				b[i] = v
			}
			snap.Histograms[s.Name] = Histogram{Counts: append([]uint64(nil), h.Counts...), Buckets: b}
		}
	}
	return snap
}
