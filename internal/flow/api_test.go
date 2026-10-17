package flow

import (
	"path/filepath"
	"testing"
)

func TestRuntimeNamesMatchStackTraceFormat(t *testing.T) {
	p := app(t)
	for q, want := range map[string]string{
		"client.(*ClientService).GetClientByID": "example.com/app/client.(*ClientService).GetClientByID",
		"algo.Square.Area":                      "example.com/app/algo.Square.Area",
		"algo.Fact":                             "example.com/app/algo.Fact",
	} {
		fn, err := p.Resolve(q)
		if err != nil {
			t.Fatal(err)
		}
		if got := RuntimeName(fn); got != want {
			t.Errorf("RuntimeName(%s) = %s, want %s", q, got, want)
		}
	}
	// The closure started by StartWorkers is StartWorkers.func1 at runtime.
	start, _ := p.Resolve("algo.StartWorkers")
	if len(start.AnonFuncs) != 1 || RuntimeName(start.AnonFuncs[0]) != "example.com/app/algo.StartWorkers.func1" {
		t.Errorf("closure runtime name: %v", start.AnonFuncs)
	}
	if NormalizeRuntimeName("example.com/x.(*List[...]).Push") != "example.com/x.(*List).Push" ||
		NormalizeRuntimeName("example.com/x.Map[go.shape.int]") != "example.com/x.Map" {
		t.Error("generic shape arguments not stripped")
	}
}

func TestFuncForFramePrefersPositionThenName(t *testing.T) {
	p := app(t)
	abs := filepath.Join(p.Dir, "algo", "algo.go")
	// Line 42 is inside the closure of StartWorkers: position wins over name.
	fn := p.FuncForFrame("whatever", abs, 42)
	if fn == nil || fn.Parent() == nil || fn.Parent().Name() != "StartWorkers" {
		t.Fatalf("position lookup: %v", fn)
	}
	// -trimpath style path (module path prefix).
	if fn := p.FuncForFrame("", "example.com/app/algo/algo.go", 5); fn == nil || fn.Name() != "Fact" {
		t.Fatalf("trimpath lookup: %v", fn)
	}
	// Name fallback when there is no usable position.
	if fn := p.FuncForFrame("example.com/app/algo.Pong", "", 0); fn == nil || fn.Name() != "Pong" {
		t.Fatalf("name lookup: %v", fn)
	}
	if fn := p.FuncForFrame("runtime.goexit", "/usr/lib/go/src/runtime/asm_amd64.s", 1700); fn != nil {
		t.Fatalf("external frame mapped to %v", fn)
	}
}

func TestCallersCarryInstructions(t *testing.T) {
	p := app(t)
	fn, _ := p.Resolve("client.ValidateClient")
	callers := p.CallersOf(fn)
	if len(callers) == 0 {
		t.Fatal("no callers")
	}
	for _, c := range callers {
		if c.Instr == nil || c.Instr.Parent() != c.Caller {
			t.Errorf("caller site without its instruction: %+v", c)
		}
	}
	if p.TypesInfo(fn) == nil || p.Package(fn) == nil {
		t.Error("type information not kept")
	}
	ref := p.Ref(fn)
	if ref.ID != fn.String() || ref.Module != "example.com/app" || ref.Version == "" || ref.EndLine < ref.Line {
		t.Errorf("ref: %+v", ref)
	}
}

func TestEnclosingModuleFindsTestdataModules(t *testing.T) {
	root, _ := filepath.Abs(".")
	dir := filepath.Join(root, "testdata", "app", "client")
	if got := EnclosingModule(dir, root); got != filepath.Join(root, "testdata", "app") {
		t.Fatalf("EnclosingModule = %q", got)
	}
	if got := EnclosingModule(root, root); got != "" {
		t.Fatalf("no go.mod expected at %s, got %q", root, got)
	}
}
