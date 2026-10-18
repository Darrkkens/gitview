package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/athos/gitview/internal/flow"
)

func runFlowCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runFlow(append(args, "../../internal/flow/testdata/app"), &out, &errb)
	return out.String(), errb.String(), code
}

func TestFlowCLIAmbiguousNameFailsNonInteractively(t *testing.T) {
	out, errs, code := runFlowCLI(t, "--function", "GetClientByID", "--non-interactive")
	if code != 3 || out != "" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	for _, want := range []string{"ambiguous", "3 symbols", "catalog.GetClientByID", "client.(*ClientService).GetClientByID", "httpapi.(*ClientHandler).GetClientByID", "client/service.go:23"} {
		if !strings.Contains(errs, want) {
			t.Errorf("stderr missing %q:\n%s", want, errs)
		}
	}
}

func TestFlowCLIDirections(t *testing.T) {
	out, _, code := runFlowCLI(t, "--function", "client.(*ClientService).GetClientByID", "--direction", "both", "--depth", "5")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	for _, want := range []string{"Callers (profundidade ≤ 5)", "← httpapi.(*ClientHandler).GetClientByID", "← GET /api/clients/:id",
		"Callees (profundidade ≤ 5)", "→ client.ValidateClient", "→ client.(*PGRepo).Find", "[provável]", "SQL: SELECT id, name FROM clients"} {
		if !strings.Contains(out, want) {
			t.Errorf("both output missing %q:\n%s", want, out)
		}
	}
	callers, _, _ := runFlowCLI(t, "--function", "client.(*ClientService).GetClientByID", "--direction", "callers")
	if strings.Contains(callers, "Callees") || !strings.Contains(callers, "Callers") {
		t.Errorf("callers output:\n%s", callers)
	}
	callees, _, _ := runFlowCLI(t, "--function", "client.(*ClientService).GetClientByID", "--direction", "callees")
	if strings.Contains(callees, "Callers") || !strings.Contains(callees, "Callees") {
		t.Errorf("callees output:\n%s", callees)
	}
}

func TestFlowCLIJSONAndValidation(t *testing.T) {
	out, _, code := runFlowCLI(t, "--function", "algo.L1", "--direction", "callees", "--depth", "5", "--json")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	var g flow.Graph
	if err := json.Unmarshal([]byte(out), &g); err != nil {
		t.Fatal(err)
	}
	if g.Node("example.com/app/algo.L6") == nil || g.Node("example.com/app/algo.L7") != nil || g.MaxDepth != 5 {
		t.Errorf("depth not respected in JSON: %d nodes", len(g.Nodes))
	}
	if _, _, code := runFlowCLI(t, "--function", "x", "--direction", "sideways"); code != 2 {
		t.Errorf("invalid direction exit %d", code)
	}
	if _, _, code := runFlowCLI(t, "--function", "x", "--depth", "0"); code != 2 {
		t.Errorf("invalid depth exit %d", code)
	}
	if _, errs, code := runFlowCLI(t, "--function", "NoSuchFunc"); code != 1 || !strings.Contains(errs, "nenhuma função") {
		t.Errorf("missing function: code %d %q", code, errs)
	}
}

func TestFlowCLIFindAndRoutes(t *testing.T) {
	out, _, _ := runFlowCLI(t, "--find", "GetClient")
	if strings.Count(out, "GetClientByID") != 3 {
		t.Errorf("find output:\n%s", out)
	}
	out, _, _ = runFlowCLI(t, "--routes", "--depth", "1")
	for _, want := range []string{"GET /api/clients/:id", "GET /v1/ping", "GET /admin/users", "GET /health"} {
		if !strings.Contains(out, want) {
			t.Errorf("routes output missing %q", want)
		}
	}
}
