package perf

import "testing"

func tinyProfile(fn string, v int64, labels map[string]string) *Profile {
	f := &Function{ID: 1, Name: fn, File: "/src/x.go"}
	return &Profile{
		SampleTypes:   []ValueType{{"samples", "count"}, {"cpu", "nanoseconds"}},
		Functions:     map[uint64]*Function{1: f},
		Locations:     map[uint64]*Location{1: {ID: 1, Lines: []Line{{Func: f, Line: 3}}}},
		Samples:       []Sample{{Locs: []uint64{1}, Values: []int64{v, v * 10}, Labels: labels}},
		DurationNanos: 5,
	}
}

func TestCombineAndFilter(t *testing.T) {
	a := tinyProfile("main.a", 4, map[string]string{"gitview_trace_id": "r1"})
	b := tinyProfile("main.b", 6, nil)
	sum, err := Combine([]*Profile{a, b}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := Analyze(sum, nil, Options{SampleType: "cpu"})
	if r.Total != 100 || len(r.Funcs) != 2 || sum.DurationNanos != 10 {
		t.Errorf("merged: total=%d funcs=%d", r.Total, len(r.Funcs))
	}
	// Subtracting a start snapshot from an end snapshot.
	end := tinyProfile("main.a", 9, nil)
	start := tinyProfile("main.a", 4, nil)
	d, _ := Combine([]*Profile{end, start}, []int64{1, -1})
	r, _ = Analyze(d, nil, Options{SampleType: "samples"})
	if r.Total != 5 || r.Funcs[0].Flat != 5 {
		t.Errorf("delta: %+v", r.Funcs)
	}
	only := sum.Filter(func(s Sample) bool { return s.Labels["gitview_trace_id"] == "r1" })
	r, _ = Analyze(only, nil, Options{SampleType: "cpu"})
	if r.Total != 40 || len(r.Funcs) != 1 || r.Funcs[0].Name != "main.a" {
		t.Errorf("filtered: %+v", r.Funcs)
	}
	if _, err := Combine([]*Profile{a, {SampleTypes: []ValueType{{"x", "y"}}}}, nil); err == nil {
		t.Error("different kinds combined")
	}
}
