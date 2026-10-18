package audit

import (
	"go/constant"
	"go/types"
	"regexp"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/inspect/sqlops"
	"golang.org/x/tools/go/ssa"
)

// callInfo is a call site of a module function with its callee resolved.
type callInfo struct {
	p    *flow.Program
	fn   *ssa.Function
	cs   flow.CallSite
	name string      // full name of the callee: os/exec.Command, (*net/http.Request).FormValue
	recv ssa.Value   // receiver of a method call
	args []ssa.Value // arguments, receiver excluded
}

// eachCall visits every call site of the module functions.
func (s *scanner) eachCall(visit func(c callInfo)) {
	for _, p := range s.in.Programs {
		for _, fn := range p.Funcs() {
			if !p.InModule(fn) {
				continue
			}
			for _, cs := range p.CallSites(fn) {
				if cs.Callback {
					continue
				}
				c := cs.Instr.Common()
				name := calleeFull(c)
				if name == "" {
					continue
				}
				recv, args := splitArgs(c)
				visit(callInfo{p: p, fn: fn, cs: cs, name: name, recv: recv, args: args})
			}
		}
	}
}

func calleeFull(c *ssa.CallCommon) string {
	if c.IsInvoke() {
		return c.Method.FullName()
	}
	if fn := c.StaticCallee(); fn != nil {
		if obj, ok := fn.Object().(*types.Func); ok {
			return obj.FullName()
		}
		return fn.String()
	}
	return ""
}

func splitArgs(c *ssa.CallCommon) (ssa.Value, []ssa.Value) {
	if c.IsInvoke() {
		return c.Value, c.Args
	}
	if fn := c.StaticCallee(); fn != nil && fn.Signature.Recv() != nil && len(c.Args) > 0 {
		return c.Args[0], c.Args[1:]
	}
	return nil, c.Args
}

// constString returns the value of a string constant argument.
func constString(v ssa.Value) (string, bool) {
	for {
		switch x := v.(type) {
		case *ssa.MakeInterface:
			v = x.X
			continue
		case *ssa.Const:
			if x.Value != nil && x.Value.Kind() == constant.String {
				return constant.StringVal(x.Value), true
			}
		}
		return "", false
	}
}

func constBool(v ssa.Value) (bool, bool) {
	if k, ok := v.(*ssa.Const); ok && k.Value != nil && k.Value.Kind() == constant.Bool {
		return constant.BoolVal(k.Value), true
	}
	return false, false
}

// callText is the source of the call, on one line.
func (c callInfo) text() string {
	if ce := c.p.CallExprAt(c.fn, c.cs.Instr.Pos()); ce != nil {
		return normCode(c.p.NodeText(ce))
	}
	return normCode(c.p.Source(c.cs.File, c.cs.Line, c.cs.Line))
}

// taintFinding reports an untrusted value reaching a sensitive argument;
// it returns false when nothing untrusted reaches it.
func (s *scanner) taintFinding(r *rule, c callInfo, v ssa.Value, class, sink, argName string, sev string) bool {
	t := s.trace(c.p, c.fn, v, argName)
	paths := t.exploitable(class)
	if len(paths) == 0 {
		return false
	}
	f := s.newFinding(r, c.p, c.fn, c.cs.File, c.cs.Line, c.text())
	f.Severity = sev
	f.Confidence = findingConfidence(paths)
	f.Message = sink + ": " + argName + " comes from " + paths[0].steps[0].Label + "."
	s.addFlow(f, t, paths)
	f.ev(Evidence{Kind: EvSink, Text: sink, File: c.cs.File, Line: c.cs.Line, Code: code(c.p, c.cs.File, c.cs.Line), Symbol: f.SymbolID, Confidence: evidence.Exact, Source: "go/ssa"})
	f.Flow = append(f.Flow, FlowStep{Role: "sink", Label: sink, Func: flow.ShortName(flow.Origin(c.fn)), File: c.cs.File, Line: c.cs.Line, Code: code(c.p, c.cs.File, c.cs.Line)})
	f.Verdict = Review
	if f.Confidence == ConfConfirmed && len(t.validated) == 0 {
		f.Verdict = Vulnerable
	}
	if len(t.validated) > 0 {
		f.Message += " A condition on the value was found; whether it is sufficient is not established."
	}
	guarded(f, guards(c.p, c.cs.Instr, v))
	s.linkRoutes(f)
	s.add(f)
	return true
}

// incompleteFinding reports a sensitive value whose origin could not be
// followed, when the operation is reachable from a route.
func (s *scanner) incompleteFinding(r *rule, c callInfo, t *taint, sink string) {
	f := s.newFinding(r, c.p, c.fn, c.cs.File, c.cs.Line, c.text())
	s.linkRoutes(f)
	if len(f.Routes) == 0 {
		return
	}
	f.Title = "SQL built at runtime with an undetermined origin"
	f.Severity, f.Confidence, f.Verdict = SevInfo, ConfPossible, Incomplete
	f.Message = sink + " is built at runtime and Data Flow could not follow every part of it to its origin."
	s.addFlow(f, t, nil)
	f.ev(Evidence{Kind: EvSink, Text: sink, File: c.cs.File, Line: c.cs.Line, Code: code(c.p, c.cs.File, c.cs.Line), Symbol: f.SymbolID, Confidence: evidence.Exact, Source: "go/ssa"})
	for _, u := range t.unresolved {
		f.ev(Evidence{Kind: EvAbsence, Text: "trace stopped: " + u, Source: "dataflow"})
	}
	f.limit("the origin of part of the value is unresolved")
	s.add(f)
}

// ---------- GO-INJ-001 ----------

func ruleSQLInjection(s *scanner, r *rule) {
	for _, p := range s.in.Programs {
		for _, op := range sqlops.All(p) {
			v := op.SQLValue()
			if v == nil {
				continue
			}
			switch op.SQL.Kind {
			case sqlops.TextConcat, sqlops.TextSprintf, sqlops.TextDynamic, sqlops.TextParameter:
			default:
				continue // literals, constants and alternatives of literals
			}
			c := callInfo{p: p, fn: op.Function(), cs: flow.CallSite{Instr: op.Instr(), File: op.File, Line: op.Line}}
			sink := op.Lib + " " + op.Method + " with SQL built by " + op.SQL.Kind
			if op.SQL.Template != "" {
				sink += ": " + oneLine(op.SQL.Template, 140)
			}
			if !s.taintFinding(r, c, v, "sql", sink, "the SQL text", r.Severity) {
				t := s.trace(p, op.Function(), v, "the SQL text")
				if len(t.untrusted) == 0 && len(t.unresolved) > 0 {
					s.incompleteFinding(r, c, t, sink)
				}
			}
		}
	}
}

func oneLine(s string, n int) string {
	s = normCode(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// ---------- GO-INJ-002 ----------

var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "/bin/sh": true, "/bin/bash": true, "/usr/bin/env": false, "cmd": true, "cmd.exe": true, "powershell": true, "pwsh": true}

func ruleCommandInjection(s *scanner, r *rule) {
	s.eachCall(func(c callInfo) {
		var name ssa.Value
		var rest []ssa.Value
		switch c.name {
		case "os/exec.Command":
			if len(c.args) < 1 {
				return
			}
			name = c.args[0]
			if len(c.args) > 1 {
				rest, _ = flow.VariadicElems(c.args[1])
			}
		case "os/exec.CommandContext":
			if len(c.args) < 2 {
				return
			}
			name = c.args[1]
			if len(c.args) > 2 {
				rest, _ = flow.VariadicElems(c.args[2])
			}
		default:
			return
		}
		prog, isConst := constString(name)
		if !isConst {
			s.taintFinding(r, c, name, "command", "program name of "+shortCall(c.name), "the program name", SevCritical)
			return
		}
		if shells[prog] {
			for k, a := range rest {
				if flag, ok := constString(a); ok && (flag == "-c" || flag == "/c" || flag == "-Command") && k+1 < len(rest) {
					s.taintFinding(r, c, rest[k+1], "command", "shell script run by "+prog+" "+flag, "the shell script", SevCritical)
					return
				}
			}
		}
		for _, a := range rest {
			if _, ok := constString(a); ok {
				continue
			}
			if s.taintFinding(r, c, a, "command", "argument of "+prog, "an argument", SevHigh) {
				return
			}
		}
	})
}

var pkgPathPrefix = regexp.MustCompile(`[A-Za-z0-9_.\-]+/`)

// shortCall drops import paths: (*github.com/gin-gonic/gin.Context).Redirect
// becomes (*gin.Context).Redirect, os/exec.Command exec.Command.
func shortCall(full string) string { return pkgPathPrefix.ReplaceAllString(full, "") }

// ---------- GO-INJ-003 ----------

var fileSinks = map[string][]int{
	"os.Open": {0}, "os.OpenFile": {0}, "os.ReadFile": {0}, "os.WriteFile": {0}, "os.Create": {0}, "os.Remove": {0},
	"os.RemoveAll": {0}, "os.Mkdir": {0}, "os.MkdirAll": {0}, "os.Rename": {0, 1}, "os.ReadDir": {0}, "os.Chmod": {0},
	"io/ioutil.ReadFile": {0}, "io/ioutil.WriteFile": {0}, "net/http.ServeFile": {2},
	"(*github.com/gin-gonic/gin.Context).File": {0}, "(*github.com/gin-gonic/gin.Context).FileAttachment": {0},
	"(github.com/labstack/echo/v4.Context).File": {0}, "(github.com/labstack/echo/v4.Context).Attachment": {0},
}

func rulePathTraversal(s *scanner, r *rule) {
	s.eachCall(func(c callInfo) {
		idx, ok := fileSinks[c.name]
		if !ok {
			return
		}
		for _, i := range idx {
			if i < len(c.args) {
				if _, isConst := constString(c.args[i]); !isConst && s.taintFinding(r, c, c.args[i], "path", "file path of "+shortCall(c.name), "the file path", r.Severity) {
					return
				}
			}
		}
	})
}

// ---------- GO-INJ-004 ----------

var redirectSinks = map[string]int{
	"net/http.Redirect": 2, "(*github.com/gin-gonic/gin.Context).Redirect": 1, "(github.com/labstack/echo/v4.Context).Redirect": 1,
}

func ruleOpenRedirect(s *scanner, r *rule) {
	s.eachCall(func(c callInfo) {
		i, ok := redirectSinks[c.name]
		if !ok || i >= len(c.args) {
			return
		}
		if _, isConst := constString(c.args[i]); isConst {
			return
		}
		before := len(s.audit.Findings)
		if s.taintFinding(r, c, c.args[i], "url", "redirect location of "+shortCall(c.name), "the location", r.Severity) {
			// Many redirects to input are meant for relative paths: a
			// person has to tell, so this stays a review.
			f := s.audit.Findings[before]
			f.Verdict, f.VerdictLabel = Review, ""
			f.VerdictLabel = verdictLabel(f)
		}
	})
}
