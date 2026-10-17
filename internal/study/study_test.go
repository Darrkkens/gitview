package study

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect"
	"github.com/athos/gitview/internal/inspect/branch"
	"github.com/athos/gitview/internal/inspect/perf"
	"github.com/athos/gitview/internal/inspect/pgexplain"
	"github.com/athos/gitview/internal/inspect/xtrace"
)

var (
	shopOnce sync.Once
	shopProg *flow.Program
	shopErr  error
)

func shopDir() string {
	_, file, _, _ := runtime.Caller(0)
	d, _ := filepath.Abs(filepath.Join(filepath.Dir(file), "testdata", "shop"))
	return d
}

// shop loads the fixture module with a simulated working-tree diff.
func shop(t *testing.T) Inputs {
	t.Helper()
	shopOnce.Do(func() {
		shopProg, shopErr = flow.Load(context.Background(), shopDir(), shopDir())
	})
	if shopErr != nil {
		t.Fatal(shopErr)
	}
	lines := func(from, to int) map[int]bool {
		m := map[int]bool{}
		for l := from; l <= to; l++ {
			m[l] = true
		}
		return m
	}
	changed := map[string]map[int]bool{
		"internal/services/rating.go":        lines(32, 38), // RateUsage: the overage branch
		"internal/handlers/usage_handler.go": lines(22, 22), // GetCharge
		"internal/models/usage.go":           lines(10, 10),
		"internal/services/rating_test.go":   lines(5, 7),
	}
	changed["internal/services/rating.go"][49] = true // calculateOverage
	g := shopProg.Index(flow.Options{Changed: changed})
	return Inputs{
		Root: shopDir(), Programs: []*flow.Program{shopProg}, Graph: g, Changed: changed,
		Files: []FileChange{
			{Path: "internal/handlers/usage_handler.go", Status: "M", Added: 1, Deleted: 1},
			{Path: "internal/models/usage.go", Status: "M", Added: 1},
			{Path: "internal/services/rating.go", Status: "M", Added: 7, Deleted: 2},
			{Path: "internal/services/rating_test.go", Status: "M", Added: 3},
			{Path: "migrations/0002_plan_overage.sql", Status: "A", Added: 2},
			{Path: "web/PlanForm.vue", Status: "M", Added: 1, Deleted: 1},
		},
		Tests: []TestResult{{Package: "example.com/shop/internal/services", Name: "TestOverage", Status: "passed"}},
	}
}

func roleOf(m *Model, name string) *FuncRole {
	for i, f := range m.Architecture.Functions {
		if f.Name == name {
			return &m.Architecture.Functions[i]
		}
	}
	return nil
}

func TestArchitectureIsClassifiedWithEvidence(t *testing.T) {
	m := Build(shop(t))
	want := map[string][2]string{
		"handlers.(*UsageHandler).GetCharge":       {LayerHandler, Static},
		"services.(*RatingService).RateUsage":      {LayerService, Static},
		"repository.(*UsageRepository).FindUsage":  {LayerRepository, Static},
		"repository.(*UsageRepository).SaveCharge": {LayerRepository, Static},
		"services.(*PricingClient).OverageRate":    {LayerExternal, Static},
		"main.main":                                {LayerEntrypoint, Static},
		"services.calculateOverage":                {LayerService, Heuristic},
	}
	for name, w := range want {
		r := roleOf(m, name)
		if r == nil {
			t.Errorf("%s not classified", name)
			continue
		}
		if r.Layer != w[0] || r.Basis != w[1] || len(r.Evidence) == 0 {
			t.Errorf("%s: %s/%s, want %s/%s (%+v)", name, r.Layer, r.Basis, w[0], w[1], r.Evidence)
		}
	}
	pkgs := map[string]PackageInfo{}
	for _, p := range m.Architecture.Packages {
		pkgs[p.Name] = p
	}
	if p := pkgs["models"]; p.Layer != LayerModel || p.Basis != Heuristic {
		t.Errorf("models package: %+v", p)
	}
	if p := pkgs["routes"]; p.Layer != LayerRoute || p.Basis != Static {
		t.Errorf("routes package: %+v", p)
	}
	if len(m.Architecture.Chains) != 1 {
		t.Fatalf("chains: %+v", m.Architecture.Chains)
	}
	var layers []string
	for _, s := range m.Architecture.Chains[0].Steps {
		layers = append(layers, s.Layer)
	}
	if got := strings.Join(layers, " → "); got != "HTTP Route → Handler → Service → Repository → Database" {
		t.Errorf("chain: %s", got)
	}
}

func TestChangeJourney(t *testing.T) {
	m := Build(shop(t))
	got := map[string]string{}
	for _, g := range m.Changes {
		for _, f := range g.Files {
			got[f.Path] = g.Layer + "/" + f.Basis
		}
	}
	want := map[string]string{
		"internal/handlers/usage_handler.go": "Handler/STATIC",
		"internal/services/rating.go":        "Service/STATIC",
		"internal/models/usage.go":           "Model/HEURISTIC",
		"internal/services/rating_test.go":   "Tests/STATIC",
		"migrations/0002_plan_overage.sql":   "Database/HEURISTIC",
		"web/PlanForm.vue":                   "Frontend/HEURISTIC",
	}
	for p, w := range want {
		if got[p] != w {
			t.Errorf("%s: %s, want %s", p, got[p], w)
		}
	}
	var order []string
	for _, g := range m.Changes {
		order = append(order, g.Layer)
	}
	if strings.Join(order, ",") != "Handler,Service,Database,Model,Tests,Frontend" {
		t.Errorf("group order: %v", order)
	}
}

func TestStartHereIsReproducible(t *testing.T) {
	in := shop(t)
	m := Build(in)
	var got []string
	for _, s := range m.ReadingOrder {
		got = append(got, s.Kind+":"+s.Title)
	}
	want := []string{
		"route:GET /api/charges/{company}",
		"function:handlers.(*UsageHandler).GetCharge",
		"function:services.(*RatingService).RateUsage",
		"function:services.calculateOverage",
		"function:repository.(*UsageRepository).FindUsage",
		"function:services.(*PricingClient).OverageRate",
		"function:repository.(*UsageRepository).SaveCharge",
		"sql:0002_plan_overage.sql",
		"test:TestOverage",
		"test:TestWithinQuota",
		"test:TestRateUsageNeedsRepository",
		"file:usage.go",
		"file:PlanForm.vue",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("reading order:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, s := range m.ReadingOrder {
		if len(s.Why) == 0 {
			t.Errorf("step without evidence: %+v", s)
		}
	}
	// Equal inputs, equal model.
	again := Build(in)
	a, _ := json.Marshal(m)
	b, _ := json.Marshal(again)
	if string(a) != string(b) || m.Fingerprint == "" || m.Fingerprint != again.Fingerprint {
		t.Error("the model is not deterministic")
	}
	if ids := Plan(in); len(ids) != 6 || ids[0] != "(*example.com/shop/internal/handlers.UsageHandler).GetCharge" {
		t.Errorf("plan: %v", ids)
	}
}

func TestTestReferences(t *testing.T) {
	m := Build(shop(t))
	if m.Summary.TestFunctions != len(m.Tests) || len(m.Tests) == 0 {
		t.Errorf("summary counts %d test functions, model has %d", m.Summary.TestFunctions, len(m.Tests))
	}
	refs := map[string]string{}
	for _, tf := range m.Tests {
		for _, r := range tf.Refs {
			refs[tf.Name+"→"+r.Name] = r.Basis
		}
		if tf.Name == "TestOverage" && tf.Status != "passed" {
			t.Errorf("status of TestOverage: %q", tf.Status)
		}
	}
	if refs["TestOverage→services.calculateOverage"] != Static || refs["TestRateUsageNeedsRepository→services.(*RatingService).RateUsage"] != Heuristic {
		t.Errorf("refs: %v", refs)
	}
}

// withBundles adds the Deep Inspector results of the reading-order
// functions, as the report pipeline does.
func withBundles(t *testing.T, in Inputs) Inputs {
	t.Helper()
	sess := inspect.NewSession(in.Root, in.Programs)
	if in.Coverage != "" {
		prof, err := branch.LoadProfile(filepath.Join(in.Root, in.Coverage))
		if err != nil {
			t.Fatal(err)
		}
		sess.Coverage = prof
	}
	for _, id := range Plan(in) {
		r, err := sess.Resolve(id)
		if err != nil {
			t.Fatal(err)
		}
		in.Bundles = append(in.Bundles, sess.Bundle(r, inspect.BundleOptions{DataFlow: true}))
	}
	return in
}

func TestDecisionsComeFromTheCFG(t *testing.T) {
	m := Build(withBundles(t, shop(t)))
	var rate *FuncDecisions
	for i := range m.Decisions {
		if m.Decisions[i].FuncName == "services.(*RatingService).RateUsage" {
			rate = &m.Decisions[i]
		}
	}
	if rate == nil {
		t.Fatalf("no decisions for RateUsage: %+v", m.Decisions)
	}
	byCond := map[string]Decision{}
	for _, d := range rate.Decisions {
		byCond[d.Cond] = d
	}
	zero, ok := byCond["usage.ZeroRated"]
	if !ok || zero.Evidence.Basis != Static || zero.Line != 29 {
		t.Fatalf("usage.ZeroRated: %+v (all: %v)", zero, byCond)
	}
	for _, o := range zero.Outcomes {
		if o.Label == "true" && (o.Terminal != "return" || !strings.Contains(o.First, "return charge, nil")) {
			t.Errorf("true outcome: %+v", o)
		}
	}
	nilPlan := byCond["plan == nil"]
	if len(nilPlan.Outcomes) != 2 || !nilPlan.Outcomes[0].ReturnsError && !nilPlan.Outcomes[1].ReturnsError {
		t.Errorf("plan == nil: %+v", nilPlan)
	}
	over := byCond["usage.Total > plan.Quota"]
	var assigns []string
	for _, o := range over.Outcomes {
		if o.Label == "true" {
			assigns = o.Assigns
		}
	}
	if strings.Join(assigns, ",") != "rate,err" {
		t.Errorf("assignments of the overage branch: %v", assigns)
	}
	if errCheck := byCond["err != nil"]; !errCheck.ErrorCheck {
		t.Errorf("error check: %+v", errCheck)
	}
}

func TestDataJourneysGoFromSourceToSink(t *testing.T) {
	m := Build(withBundles(t, shop(t)))
	if len(m.DataJourneys) == 0 {
		t.Fatal("no data journeys")
	}
	complete := 0
	for _, j := range m.DataJourneys {
		if len(j.Steps) < 2 || j.Evidence.Basis != Static || !strings.HasPrefix(j.Evidence.Ref, "inspect:") {
			t.Errorf("journey: %+v", j)
		}
		if j.Complete {
			complete++
			if j.Steps[0].Role != RoleSource {
				t.Errorf("complete journey without source: %+v", j.Steps[0])
			}
		}
		for i, s := range j.Steps {
			if i > 0 && s.Via == "" {
				t.Errorf("step without edge: %+v", s)
			}
		}
	}
	if complete == 0 {
		t.Errorf("no complete journey among %d", len(m.DataJourneys))
	}
	for _, j := range m.DataJourneys {
		var parts []string
		for _, s := range j.Steps {
			parts = append(parts, s.Role[:1]+":"+s.Label)
		}
		t.Logf("%s %s: %s", j.FuncName, j.Variable, strings.Join(parts, " → "))
	}
}

func TestBranchTestMatrixSeparatesStaticFromObserved(t *testing.T) {
	in := shop(t)
	in.Coverage = "cover.out"
	m := Build(withBundles(t, in))
	var calc, rate *MatrixFunc
	for i := range m.TestMatrix {
		switch m.TestMatrix[i].FuncName {
		case "services.calculateOverage":
			calc = &m.TestMatrix[i]
		case "services.(*RatingService).RateUsage":
			rate = &m.TestMatrix[i]
		}
	}
	if calc == nil || rate == nil {
		t.Fatalf("matrix: %+v", m.TestMatrix)
	}
	// calculateOverage: called directly by two tests (STATIC) and reached by
	// a third through RateUsage (HEURISTIC: a method name); both of its
	// outcomes ran (the profile is of those tests).
	if len(calc.Tests) != 3 || calc.Tests[0].Basis != Static || calc.Tests[0].Hops != 0 || calc.Tests[2].Basis != Heuristic || calc.Tests[2].Hops != 1 {
		t.Errorf("tests of calculateOverage: %+v", calc.Tests)
	}
	if calc.Observed != 2 || calc.NotObserved != 0 || calc.Coverage != "cover.out" {
		t.Errorf("calculateOverage coverage: %+v", calc)
	}
	// RateUsage: reached by a test only through a method name (HEURISTIC);
	// the test panics before the branches, so they were not observed.
	if len(rate.Tests) != 1 || rate.Tests[0].Basis != Heuristic {
		t.Errorf("tests of RateUsage: %+v", rate.Tests)
	}
	if rate.Observed != 0 || rate.NotObserved == 0 || len(rate.Unobserved) != rate.NotObserved {
		t.Errorf("RateUsage coverage: observed %d not %d unobserved %d", rate.Observed, rate.NotObserved, len(rate.Unobserved))
	}
	for _, u := range rate.Unobserved {
		if u.Path[0] != "ENTRY" || len(u.Path) < 2 {
			t.Errorf("path: %+v", u)
		}
		if u.Scenario == "usage.ZeroRated → true" && strings.Join(u.Path, " | ") != "ENTRY | err != nil → false | plan == nil → false | usage.ZeroRated → true | return charge, nil | RETURN" {
			t.Errorf("path to usage.ZeroRated: %v", u.Path)
		}
	}
	// Without a coverage profile nothing is "not observed": there is no data.
	m2 := Build(withBundles(t, shop(t)))
	for _, mf := range m2.TestMatrix {
		if mf.Observed != 0 || mf.NotObserved != 0 {
			t.Errorf("observations without coverage: %+v", mf)
		}
	}
}

func nodeID(t *testing.T, g *flow.Graph, name string) string {
	t.Helper()
	for _, n := range g.Nodes {
		if n.Name == name {
			return n.ID
		}
	}
	t.Fatalf("no node %s", name)
	return ""
}

func TestPredictComparesWithTheTrace(t *testing.T) {
	in := shop(t)
	handler := nodeID(t, in.Graph, "handlers.(*UsageHandler).GetCharge")
	rate := nodeID(t, in.Graph, "services.(*RatingService).RateUsage")
	find := nodeID(t, in.Graph, "repository.(*UsageRepository).FindUsage")
	over := nodeID(t, in.Graph, "services.calculateOverage")
	save := nodeID(t, in.Graph, "repository.(*UsageRepository).SaveCharge")
	// Stacks are innermost frame first.
	in.Trace = &xtrace.Timeline{
		Stacks: [][]xtrace.Frame{
			{{Func: "FindUsage", Symbol: find}, {Func: "RateUsage", Symbol: rate}, {Func: "GetCharge", Symbol: handler}, {Func: "net/http.HandlerFunc.ServeHTTP"}},
			{{Func: "SaveCharge", Symbol: save}, {Func: "RateUsage", Symbol: rate}, {Func: "GetCharge", Symbol: handler}},
			{{Func: "calculateOverage", Symbol: over}, {Func: "RateUsage", Symbol: rate}, {Func: "GetCharge", Symbol: handler}},
			{{Func: "SaveCharge", Symbol: save}, {Func: "main", Symbol: "elsewhere"}},
		},
		// SaveCharge runs first outside the entry point: it does not count.
		Events:  []xtrace.Event{{T: 300, Kind: "block", Stack: 1}, {T: 100, Kind: "block", Stack: 0}, {T: 50, Kind: "block", Stack: 3}},
		Regions: []*xtrace.Region{{Type: "calculateOverage", Start: 200, End: 210, Stack: 2, Instrumented: true, Symbol: over}},
	}
	in.Trace.Artifact.Name = "trace.out"
	m := Build(in)

	var en *PredictEntry
	for i := range m.Predict.Entries {
		if m.Predict.Entries[i].ID == handler {
			en = &m.Predict.Entries[i]
		}
	}
	if en == nil {
		t.Fatalf("no predict entry for the route handler: %+v", m.Predict.Entries)
	}
	cands := map[string]int{}
	for _, c := range en.Candidates {
		cands[c.ID] = c.Depth
	}
	for id, d := range map[string]int{rate: 1, find: 2, over: 2, save: 2} {
		if cands[id] != d {
			t.Errorf("candidate %s depth %d, want %d", id, cands[id], d)
		}
	}
	if len(en.StaticOrder) < 2 || en.StaticOrder[0] != handler || en.StaticOrder[1] != rate {
		t.Errorf("static order %v", en.StaticOrder)
	}

	if m.Predict.Observed == nil || m.Predict.Observed.Source != "trace.out" {
		t.Fatalf("observed run: %+v", m.Predict.Observed)
	}
	obs := en.Observed
	var got []string
	for _, c := range obs {
		got = append(got, c.ID)
	}
	want := []string{handler, rate, find, over, save}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("observed order\n got %v\nwant %v", got, want)
	}
	if obs[3].How != "region" || obs[0].How != "stack" || obs[4].FirstNs != 300 {
		t.Errorf("observed: %+v", obs)
	}

	var trace []Exercise
	for _, e := range m.Exercises {
		if e.Type == "trace" {
			trace = append(trace, e)
		}
	}
	if len(trace) != 4 {
		t.Fatalf("%d trace challenges, want 4", len(trace))
	}
	for i, e := range trace {
		if wantNext := in.Graph.Node(want[i+1]).Name; e.Options[e.Answer] != wantNext {
			t.Errorf("challenge %d answer %q, want %q", i, e.Options[e.Answer], wantNext)
		}
		if len(e.Options) < 3 || e.Evidence.Basis != Runtime || e.SourceType != "TRACE" {
			t.Errorf("challenge %d: %+v", i, e)
		}
	}
	if Build(in).Fingerprint != m.Fingerprint {
		t.Error("same inputs gave a different model")
	}
}

func TestTraceChallengesSkipTies(t *testing.T) {
	p := Predict{
		Entries:  []PredictEntry{{ID: "e", Title: "E", Candidates: []Candidate{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}, {ID: "c", Name: "C"}, {ID: "d", Name: "D"}}}},
		Observed: &ObservedRun{},
	}
	p.Entries[0].Observed = []ObservedCall{{ID: "e", Name: "E", Seq: 0}, {ID: "a", Name: "A", Seq: 1}, {ID: "b", Name: "B", Seq: 1}, {ID: "c", Name: "C", Seq: 3}}
	for _, e := range traceChallenges(p) {
		if strings.Contains(e.Prompt, "right after E?") || strings.Contains(e.Prompt, "right after A?") {
			t.Errorf("asked about a tied order: %s", e.Prompt)
		}
	}
}

func TestFmtNs(t *testing.T) {
	for ns, want := range map[int64]string{999: "999 ns", 212032: "212.0 µs", 1500000: "1.50 ms", 2500000000: "2.50 s"} {
		if got := fmtNs(ns); got != want {
			t.Errorf("fmtNs(%d) = %q, want %q", ns, got, want)
		}
	}
}

func findQuery(m *Model, funcName string) *SQLQuery {
	for i := range m.SQL.Queries {
		if m.SQL.Queries[i].FuncName == funcName {
			return &m.SQL.Queries[i]
		}
	}
	return nil
}

func TestSQLStudy(t *testing.T) {
	in := withBundles(t, shop(t))
	plain := Build(in)
	q := findQuery(plain, "repository.(*UsageRepository).FindUsage")
	if q == nil {
		t.Fatalf("no FindUsage query: %+v", plain.SQL.Queries)
	}
	if len(plain.SQL.SchemaFiles) != 2 || plain.SQL.Indexes != 4 {
		t.Errorf("schema files %v, %d indexes", plain.SQL.SchemaFiles, plain.SQL.Indexes)
	}
	s := q.Structure
	if s == nil || len(s.Tables) != 2 || s.Tables[0].Name != "usage" || s.Tables[1].Alias != "p" || len(s.Joins) != 1 || s.Joins[0].On != "p.id = u.plan_id" {
		t.Fatalf("structure %+v", s)
	}
	if strings.Join(s.Filters, " | ") != "u.company_id = $1 | u.period = $2" || strings.Join(s.OrderBy, ",") != "u.created_at DESC" || s.Limit != "1" {
		t.Errorf("clauses: %v %v %q", s.Filters, s.OrderBy, s.Limit)
	}
	refs := map[string]string{}
	for _, ix := range q.Indexes {
		refs[ix.Table+":"+strings.Join(ix.Columns, ",")] = strings.Join(ix.Referenced, ",")
		if ix.InPlan {
			t.Errorf("index %s marked as used without a plan", ix.Name)
		}
	}
	want := map[string]string{"usage:id": "", "usage:company_id,period": "company_id (WHERE),period (WHERE)", "plans:id": "id (JOIN)"}
	for k, v := range want {
		if got, ok := refs[k]; !ok || got != v {
			t.Errorf("index %s: referenced %q (present %v), want %q", k, got, ok, v)
		}
	}
	if len(q.Callers) != 1 || q.Callers[0].Name != "services.(*RatingService).RateUsage" || !q.InReport {
		t.Errorf("callers %+v in report %v", q.Callers, q.InReport)
	}
	if q.Plan != nil {
		t.Error("plan without EXPLAIN")
	}
	if ins := findQuery(plain, "repository.(*UsageRepository).SaveCharge"); ins == nil || ins.Structure.Tables[0].Role != "insert" || len(ins.Indexes) != 1 || ins.Indexes[0].Name != "idx_charges_usage" || len(ins.Indexes[0].Referenced) != 0 {
		t.Errorf("insert query %+v", ins)
	}

	// An EXPLAIN plan is the only evidence of index use.
	in.Plans = []*pgexplain.Plan{{SQL: q.SQL + "\n", Mode: "EXPLAIN", Indexes: []string{"idx_usage_company_period"}, Scans: []string{"Index Scan on usage"}}}
	withPlan := findQuery(Build(in), "repository.(*UsageRepository).FindUsage")
	if withPlan.Plan == nil || withPlan.Plan.Evidence.Basis != SQLBasis {
		t.Fatalf("plan %+v", withPlan.Plan)
	}
	for _, ix := range withPlan.Indexes {
		if ix.InPlan != (ix.Name == "idx_usage_company_period") {
			t.Errorf("index %q in plan %v", ix.Name, ix.InPlan)
		}
	}
}

func TestPerfStudyKeepsObservedAndStaticApart(t *testing.T) {
	in := withBundles(t, shop(t))
	m := Build(in)
	o := m.Performance.Observed
	if o.CPU != nil || o.Mem != nil || len(o.Benchmarks) != 0 || len(o.Missing) != 4 {
		t.Errorf("observed without data: %+v", o)
	}
	var rate *StaticPerf
	for i := range m.Performance.Static {
		if m.Performance.Static[i].FuncName == "services.(*RatingService).RateUsage" {
			rate = &m.Performance.Static[i]
		}
	}
	if rate == nil || rate.Cyclomatic < 5 || rate.DBOps != 0 || rate.DBReach != 2 || rate.EscapeRun {
		t.Errorf("static RateUsage %+v", rate)
	}

	over := nodeID(t, in.Graph, "services.calculateOverage")
	in.CPU = &perf.Report{Kind: "cpu", SampleType: "cpu", Unit: "nanoseconds", Total: 1000, Samples: 10,
		Funcs: []perf.FuncStat{{Name: "runtime.mallocgc", Flat: 600, Cum: 600}, {Name: "services.calculateOverage", Symbol: over, Flat: 300, Cum: 400, FlatPct: 30, CumPct: 40}}}
	in.CPU.Artifact.Name = "cpu.out"
	o = Build(in).Performance.Observed
	if o.CPU == nil || len(o.CPU.Top) != 2 || o.CPU.Top[0].Name != "runtime.mallocgc" || o.CPU.Evidence.Basis != Runtime {
		t.Fatalf("cpu %+v", o.CPU)
	}
	for _, f := range o.CPU.Report {
		if f.Found != (f.ID == over) {
			t.Errorf("%s found %v", f.Name, f.Found)
		}
		if f.ID == over && (f.Flat != 300 || f.CumPct != 40) {
			t.Errorf("calculateOverage %+v", f)
		}
	}
}

func TestTrimIn(t *testing.T) {
	for in, want := range map[string]string{
		"charge escapes to heap in (*UsageHandler).GetCharge": "charge escapes to heap",
		"moved to heap: u":            "moved to heap: u",
		"x escapes to heap in a loop": "x escapes to heap in a loop",
	} {
		if got := trimIn(in); got != want {
			t.Errorf("trimIn(%q) = %q", in, got)
		}
	}
}

func TestExercisesHaveOneAnswerAndEvidence(t *testing.T) {
	in := withBundles(t, shop(t))
	m := Build(in)
	types := map[string]int{}
	for _, e := range m.Exercises {
		types[e.Kind+"/"+e.Type]++
		min := 3
		if e.Type == "branch" {
			min = 2
		}
		if len(e.Options) < min || e.Answer < 0 || e.Answer >= len(e.Options) {
			t.Errorf("%s: %d options, answer %d", e.ID, len(e.Options), e.Answer)
		}
		seen := map[string]bool{}
		for _, o := range e.Options {
			if seen[o] {
				t.Errorf("%s: duplicate option %q", e.ID, o)
			}
			seen[o] = true
		}
		if e.Evidence.Basis == "" || e.Evidence.Text == "" || e.SourceType == "" {
			t.Errorf("%s without evidence: %+v", e.ID, e)
		}
	}
	for _, k := range []string{"quiz/signature", "quiz/callgraph", "quiz/cfg", "quiz/sql", "quiz/dataflow", "quiz/route", "quiz/architecture",
		"challenge/branch", "challenge/dataflow", "challenge/sql", "challenge/architecture"} {
		if types[k] == 0 {
			t.Errorf("no %s exercise (have %v)", k, types)
		}
	}
	answer := func(id string) string {
		for _, e := range m.Exercises {
			if e.ID == id {
				return e.Options[e.Answer]
			}
		}
		t.Errorf("no exercise %s", id)
		return ""
	}
	rate := nodeID(t, in.Graph, "services.(*RatingService).RateUsage")
	if got := answer("sig:" + rate); got != "(models.Charge, error)" {
		t.Errorf("RateUsage returns %q", got)
	}
	if got := answer("route:" + nodeID(t, in.Graph, "GET /api/charges/{company}")); got != "handlers.(*UsageHandler).GetCharge" {
		t.Errorf("route handler %q", got)
	}
	// Branch challenges: the answer is what the condition evaluates to.
	for _, e := range m.Exercises {
		if e.Type != "branch" || e.Code != "if usage.Total > plan.Quota" {
			continue
		}
		vals := map[string]int{}
		for _, g := range e.Given {
			var name string
			var v int
			fmt.Sscanf(strings.Replace(g, " = ", " ", 1), "%s %d", &name, &v)
			vals[name] = v
		}
		want := "false"
		if vals["usage.Total"] > vals["plan.Quota"] {
			want = "true"
		}
		if got := strings.SplitN(e.Options[e.Answer], " ", 2)[0]; got != want {
			t.Errorf("%v: answer %s, want %s", e.Given, got, want)
		}
	}
	b := Build(in)
	if b.Fingerprint != m.Fingerprint {
		t.Error("exercises are not deterministic")
	}
}

func TestBranchConditionsAreTypeChecked(t *testing.T) {
	in := shop(t)
	rate := nodeID(t, in.Graph, "services.(*RatingService).RateUsage")
	over := nodeID(t, in.Graph, "services.calculateOverage")
	cases := []struct {
		fn, cond string
		line     int
		ok       bool
	}{
		{rate, "usage.Total > plan.Quota", 32, true},
		{rate, "usage.ZeroRated", 29, true},
		{rate, "err != nil", 22, false},  // not an integer or boolean
		{rate, "plan == nil", 25, false}, // pointer
		{over, "excess <= 0 || rate == 0", 48, true},
		{rate, "usage.Total > plan.Quota", 31, false}, // wrong line: not found
	}
	for _, c := range cases {
		e, ok := condOf(in.Programs, c.fn, c.cond, c.line)
		if ok != c.ok {
			t.Errorf("%s at %d: ok %v", c.cond, c.line, ok)
			continue
		}
		if !ok {
			continue
		}
		yes, no, given := e.scenarios("seed")
		if yes == nil || no == nil {
			t.Errorf("%s: no scenario for both outcomes", c.cond)
			continue
		}
		if _, v := e.eval(*yes); !v {
			t.Errorf("%s: %v should be true", c.cond, given[yes])
		}
		if _, v := e.eval(*no); v {
			t.Errorf("%s: %v should be false", c.cond, given[no])
		}
	}
}

func TestOptionsNeedReliableAlternatives(t *testing.T) {
	if _, _, ok := options("x", "a", []string{"a", "b", ""}, 4); ok {
		t.Error("question with a single distractor")
	}
	opts, ans, ok := options("x", "a", []string{"b", "c", "d", "e", "b"}, 4)
	if !ok || len(opts) != 4 || opts[ans] != "a" {
		t.Errorf("options %v answer %d", opts, ans)
	}
	again, _, _ := options("x", "a", []string{"b", "c", "d", "e", "b"}, 4)
	if strings.Join(opts, ",") != strings.Join(again, ",") {
		t.Error("option order is not deterministic")
	}
}

func TestExplainItYourselfFacts(t *testing.T) {
	in := withBundles(t, shop(t))
	m := Build(in)
	facts := map[string]map[string][]string{}
	for _, f := range m.Explain {
		facts[f.FuncName] = map[string][]string{}
		for _, it := range f.Items {
			facts[f.FuncName][it.Key] = it.Facts
		}
	}
	rate := facts["services.(*RatingService).RateUsage"]
	has := func(list []string, sub string) bool {
		for _, s := range list {
			if strings.Contains(s, sub) {
				return true
			}
		}
		return false
	}
	if !has(rate["callers"], "handlers.(*UsageHandler).GetCharge") || !has(rate["calls"], "repository.(*UsageRepository).FindUsage") ||
		len(rate["sql"]) != 0 || !has(rate["decisions"], "usage.Total > plan.Quota") || rate["returns"][0] != "(models.Charge, error)" {
		t.Errorf("RateUsage facts %v", rate)
	}
	find := facts["repository.(*UsageRepository).FindUsage"]
	if !has(find["sql"], "usage, plans") || !has(find["calls"], "(database)") {
		t.Errorf("FindUsage facts %v", find)
	}
	if !has(facts["handlers.(*UsageHandler).GetCharge"]["callers"], "handles GET /api/charges/{company}") {
		t.Errorf("GetCharge callers %v", facts["handlers.(*UsageHandler).GetCharge"]["callers"])
	}
}

func TestBuildWithoutGoModule(t *testing.T) {
	m := Build(Inputs{Root: t.TempDir(), Files: []FileChange{{Path: "README.md", Status: "M", Added: 1}}})
	if m == nil || len(m.Notes) == 0 || len(m.Exercises) != 0 || m.Fingerprint == "" {
		t.Fatalf("model without a module: %+v", m)
	}
}
