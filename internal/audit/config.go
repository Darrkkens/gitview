package audit

import (
	"go/ast"
	"go/constant"
	"go/types"
	"strconv"
	"strings"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
)

// literalFields returns the keyed fields of a composite literal and the
// fields assigned later through the variable holding it.
func literalFields(info *types.Info, stack []ast.Node, lit *ast.CompositeLit) (map[string]ast.Expr, map[string]bool) {
	set := map[string]ast.Expr{}
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok {
				set[id.Name] = kv.Value
			}
		}
	}
	return set, assignedFields(info, stack, lit)
}

func boolConst(info *types.Info, e ast.Expr) (bool, bool) {
	if tv, ok := info.Types[e]; ok && tv.Value != nil && tv.Value.Kind() == constant.Bool {
		return constant.BoolVal(tv.Value), true
	}
	return false, false
}

// eachLiteral visits the composite literals of the given types.
func (s *scanner) eachLiteral(types_ map[string]bool, visit func(p *flow.Program, info *types.Info, stack []ast.Node, lit *ast.CompositeLit, typ, rel string, line int)) {
	s.eachFile(func(p *flow.Program, pkg *packages.Package, file *ast.File, rel string) {
		var stack []ast.Node
		ast.Inspect(file, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			if lit, ok := n.(*ast.CompositeLit); ok {
				if t := typeName(pkg.TypesInfo, lit); types_[t] {
					visit(p, pkg.TypesInfo, stack, lit, t, rel, p.Fset().Position(lit.Pos()).Line)
				}
			}
			return true
		})
	})
}

func (s *scanner) configFinding(r *rule, p *flow.Program, rel string, line int, anchor, msg, ev string, sev, conf, verdict string) *Finding {
	f := s.newFinding(r, p, p.FuncAt(rel, line), rel, line, anchor)
	f.File, f.Line = rel, line
	f.Severity, f.Confidence, f.Verdict = sev, conf, verdict
	f.Message = msg
	f.ev(Evidence{Kind: EvConfig, Text: ev, File: rel, Line: line, Code: code(p, rel, line), Symbol: f.SymbolID, Confidence: evidence.Exact, Source: "go/types"})
	s.linkRoutes(f)
	s.add(f)
	return f
}

// ---------- GO-CONF-001 ----------

var headerSetters = map[string]bool{
	"(*github.com/gin-gonic/gin.Context).Header": true, "(net/http.Header).Set": true, "(net/http.Header).Add": true,
}

func ruleCORS(s *scanner, r *rule) {
	// credentials allowed, per function
	creds := map[*ssa.Function]bool{}
	s.eachCall(func(c callInfo) {
		if headerSetters[c.name] && len(c.args) >= 2 {
			if k, ok := constString(c.args[0]); ok && strings.EqualFold(k, "Access-Control-Allow-Credentials") {
				if v, ok := constString(c.args[1]); ok && strings.EqualFold(v, "true") {
					creds[c.fn] = true
				}
			}
		}
	})
	s.eachCall(func(c callInfo) {
		if !headerSetters[c.name] || len(c.args) < 2 {
			return
		}
		k, ok := constString(c.args[0])
		if !ok || !strings.EqualFold(k, "Access-Control-Allow-Origin") {
			return
		}
		withCreds := creds[c.fn]
		if v, ok := constString(c.args[1]); ok {
			if v != "*" {
				return
			}
			sev, verdict := SevLow, Review
			msg := "Access-Control-Allow-Origin is *: any website can read the responses (fine for public data)."
			if withCreds {
				sev, msg = SevMedium, "Access-Control-Allow-Origin is * together with Allow-Credentials: true; browsers refuse the combination, which suggests the intent is to allow any origin with credentials."
			}
			f := s.newFinding(r, c.p, c.fn, c.cs.File, c.cs.Line, c.text())
			f.Severity, f.Confidence, f.Verdict, f.Message = sev, ConfConfirmed, verdict, msg
			f.ev(Evidence{Kind: EvConfig, Text: "Access-Control-Allow-Origin: *", File: c.cs.File, Line: c.cs.Line, Code: code(c.p, c.cs.File, c.cs.Line), Symbol: f.SymbolID, Confidence: evidence.Exact, Source: "go/ssa"})
			s.linkRoutes(f)
			s.add(f)
			return
		}
		t := s.trace(c.p, c.fn, c.args[1], "the allowed origin")
		for _, path := range t.untrusted {
			if !strings.Contains(strings.ToLower(path.steps[0].Label), "origin") {
				continue
			}
			f := s.newFinding(r, c.p, c.fn, c.cs.File, c.cs.Line, c.text())
			f.Confidence = findingConfidence([]taintPath{path})
			f.Severity, f.Verdict = SevMedium, Review
			f.Message = "Access-Control-Allow-Origin echoes the request's Origin header without comparing it with an allow-list."
			if withCreds {
				f.Severity = SevHigh
				f.Message += " Allow-Credentials is true in the same function: any website can make authenticated requests and read the answers."
				if f.Confidence == ConfConfirmed && len(t.validated) == 0 {
					f.Verdict = Vulnerable
				}
			}
			s.addFlow(f, t, []taintPath{path})
			guarded(f, guards(c.p, c.cs.Instr, c.args[1]))
			f.ev(Evidence{Kind: EvSink, Text: "Access-Control-Allow-Origin set from the request", File: c.cs.File, Line: c.cs.Line, Code: code(c.p, c.cs.File, c.cs.Line), Symbol: f.SymbolID, Confidence: evidence.Exact, Source: "go/ssa"})
			if withCreds {
				f.ev(Evidence{Kind: EvConfig, Text: "Access-Control-Allow-Credentials: true in " + flow.ShortName(flow.Origin(c.fn)), File: c.cs.File, Source: "go/ssa"})
			}
			s.linkRoutes(f)
			s.add(f)
			return
		}
	})
	// cors packages
	s.eachLiteral(map[string]bool{"github.com/gin-contrib/cors.Config": true, "github.com/rs/cors.Options": true}, func(p *flow.Program, info *types.Info, stack []ast.Node, lit *ast.CompositeLit, typ, rel string, line int) {
		set, _ := literalFields(info, stack, lit)
		any := false
		if e, ok := set["AllowAllOrigins"]; ok {
			any, _ = boolConst(info, e)
		}
		for _, key := range []string{"AllowOrigins", "AllowedOrigins"} {
			if e, ok := set[key]; ok {
				if cl, ok := e.(*ast.CompositeLit); ok {
					for _, el := range cl.Elts {
						if tv, ok := info.Types[el]; ok && tv.Value != nil && constant.StringVal(tv.Value) == "*" {
							any = true
						}
					}
				}
			}
		}
		_, fn := set["AllowOriginFunc"]
		if !any && !fn {
			return
		}
		cr := false
		if e, ok := set["AllowCredentials"]; ok {
			cr, _ = boolConst(info, e)
		}
		sev := SevLow
		msg := typ + " allows any origin."
		if fn {
			msg = typ + " decides the origin with AllowOriginFunc; review that it compares with an allow-list."
		}
		if cr {
			sev = SevHigh
			msg += " AllowCredentials is true."
		}
		s.configFinding(r, p, rel, line, p.NodeText(lit), msg, typ+" with permissive origins", sev, ConfConfirmed, Review)
	})
}

// ---------- GO-CONF-002 ----------

func ruleTLS(s *scanner, r *rule) {
	s.eachLiteral(map[string]bool{"crypto/tls.Config": true}, func(p *flow.Program, info *types.Info, stack []ast.Node, lit *ast.CompositeLit, typ, rel string, line int) {
		set, assigned := literalFields(info, stack, lit)
		if e, ok := set["InsecureSkipVerify"]; ok {
			if v, ok := boolConst(info, e); ok && v {
				s.configFinding(r, p, rel, line, "InsecureSkipVerify "+p.NodeText(lit), "tls.Config disables certificate verification (InsecureSkipVerify: true).",
					"InsecureSkipVerify: true", SevHigh, ConfConfirmed, Vulnerable)
			}
		} else if assigned["InsecureSkipVerify"] {
			s.configFinding(r, p, rel, line, "InsecureSkipVerify= "+p.NodeText(lit), "InsecureSkipVerify is assigned after the tls.Config is created; its value is not checked here.",
				"InsecureSkipVerify assigned", SevMedium, ConfPossible, Review)
		}
		if e, ok := set["MinVersion"]; ok {
			if tv, ok := info.Types[e]; ok && tv.Value != nil {
				if n, ok := constant.Uint64Val(tv.Value); ok && n < 0x0303 {
					s.configFinding(r, p, rel, line, "MinVersion "+p.NodeText(lit), "tls.Config allows protocol versions below TLS 1.2 (MinVersion 0x"+strconv.FormatUint(n, 16)+").",
						"MinVersion below TLS 1.2", SevMedium, ConfConfirmed, Review)
				}
			}
		}
	})
}

// ---------- GO-CONF-003 ----------

func ruleTimeouts(s *scanner, r *rule) {
	s.eachCall(func(c callInfo) {
		switch c.name {
		case "net/http.ListenAndServe", "net/http.ListenAndServeTLS", "(*github.com/gin-gonic/gin.Engine).Run", "(*github.com/gin-gonic/gin.Engine).RunTLS":
		default:
			return
		}
		f := s.newFinding(r, c.p, c.fn, c.cs.File, c.cs.Line, c.text())
		f.Confidence, f.Verdict = ConfConfirmed, Review
		f.Message = shortCall(c.name) + " starts a server with no read or write timeouts."
		f.ev(Evidence{Kind: EvConfig, Text: shortCall(c.name) + " (no http.Server with timeouts)", File: c.cs.File, Line: c.cs.Line, Code: code(c.p, c.cs.File, c.cs.Line), Symbol: f.SymbolID, Confidence: evidence.Exact, Source: "go/ssa"})
		s.add(f)
	})
	s.eachLiteral(map[string]bool{"net/http.Server": true, "net/http.Client": true}, func(p *flow.Program, info *types.Info, stack []ast.Node, lit *ast.CompositeLit, typ, rel string, line int) {
		set, assigned := literalFields(info, stack, lit)
		has := func(k string) bool { _, ok := set[k]; return ok || assigned[k] }
		if typ == "net/http.Server" {
			if has("ReadHeaderTimeout") || has("ReadTimeout") {
				return
			}
			s.configFinding(r, p, rel, line, p.NodeText(lit), "http.Server has neither ReadHeaderTimeout nor ReadTimeout.", "http.Server without read timeouts", SevLow, ConfConfirmed, Review)
			return
		}
		if has("Timeout") {
			return
		}
		s.configFinding(r, p, rel, line, p.NodeText(lit), "http.Client has no Timeout: calls without a context deadline can block forever.", "http.Client without Timeout", SevLow, ConfLikely, Review)
	})
}

// ---------- GO-CONF-004 ----------

func ruleDebug(s *scanner, r *rule) {
	s.eachCall(func(c callInfo) {
		if c.name != "github.com/gin-gonic/gin.SetMode" || len(c.args) == 0 {
			return
		}
		if v, ok := constString(c.args[0]); ok && v == "debug" {
			f := s.newFinding(r, c.p, c.fn, c.cs.File, c.cs.Line, c.text())
			f.Confidence, f.Verdict = ConfConfirmed, Review
			f.Message = "gin runs in debug mode, set by a constant."
			f.ev(Evidence{Kind: EvConfig, Text: "gin.SetMode(gin.DebugMode)", File: c.cs.File, Line: c.cs.Line, Code: code(c.p, c.cs.File, c.cs.Line), Symbol: f.SymbolID, Confidence: evidence.Exact, Source: "go/ssa"})
			s.add(f)
		}
	})
	// net/http/pprof registers on http.DefaultServeMux when imported.
	served := false
	s.eachCall(func(c callInfo) {
		if (c.name == "net/http.ListenAndServe" || c.name == "net/http.ListenAndServeTLS") && len(c.args) >= 2 {
			if k, ok := c.args[len(c.args)-1].(*ssa.Const); ok && k.IsNil() {
				served = true
			}
		}
	})
	s.eachFile(func(p *flow.Program, pkg *packages.Package, file *ast.File, rel string) {
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "DefaultServeMux" {
				if obj := pkg.TypesInfo.ObjectOf(sel.Sel); obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "net/http" {
					served = true
				}
			}
			return true
		})
	})
	s.eachFile(func(p *flow.Program, pkg *packages.Package, file *ast.File, rel string) {
		for _, imp := range file.Imports {
			if imp.Path.Value != `"net/http/pprof"` {
				continue
			}
			line := p.Fset().Position(imp.Pos()).Line
			conf, msg := ConfPossible, "net/http/pprof is imported: it registers /debug/pprof/ on http.DefaultServeMux; whether that mux is served was not determined."
			sev := SevLow
			if served {
				conf, sev, msg = ConfLikely, SevMedium, "net/http/pprof is imported and http.DefaultServeMux is served: /debug/pprof/ (stacks, heap, profiles) is reachable on that listener."
			}
			s.configFinding(r, p, rel, line, `import "net/http/pprof"`, msg, `import "net/http/pprof"`, sev, conf, Review)
		}
	})
}
