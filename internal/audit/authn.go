package audit

import (
	"go/ast"
	"go/constant"
	"go/types"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/graph/model"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
)

// ---------- GO-AUTHN-001 ----------

// writes are the reached operations that change state.
type write struct {
	id, label, file string
	line            int
}

func (s *scanner) writesOf(rc *reach) []write {
	var out []write
	for id := range rc.prev {
		n := s.nodes[id]
		if n == nil {
			continue
		}
		switch {
		case n.Type == model.KindDB:
			op, _ := n.Metadata["op"].(string)
			sql, _ := n.Metadata["sql"].(string)
			stmt := strings.ToUpper(strings.Fields(strings.TrimSpace(sql) + " ?")[0])
			if op == "Exec" || op == "CopyFrom" || op == "SendBatch" || stmt == "INSERT" || stmt == "UPDATE" || stmt == "DELETE" || stmt == "MERGE" {
				lbl := n.Name
				if sql != "" {
					lbl += ": " + oneLine(sql, 120)
				}
				out = append(out, write{id: id, label: lbl, file: n.File, line: n.Line})
			}
		case n.Type == model.KindExternal && (strings.HasPrefix(n.Name, "exec.Command") || n.Name == "os.WriteFile" || n.Name == "os.Remove" || n.Name == "os.RemoveAll" || n.Name == "os.Create"):
			out = append(out, write{id: id, label: n.Name, file: n.File, line: n.Line})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].file < out[j].file || out[i].file == out[j].file && out[i].line < out[j].line
	})
	return out
}

func ruleUnauthenticatedWrite(s *scanner, r *rule) {
	for _, rt := range s.sortedRoutes() {
		if rt.Auth != nil && rt.Auth.Recognized {
			continue
		}
		rc := s.reachFrom(rt.ID, 10)
		ws := s.writesOf(rc)
		if len(ws) == 0 {
			continue
		}
		f := s.routeFinding(r, rt)
		f.Confidence, f.Verdict = ConfPossible, Review
		f.VerdictLabel = "Review Required — No Authentication Identified"
		var labels []string
		for _, w := range ws {
			labels = append(labels, w.label)
		}
		f.Message = rt.Key + " has no recognised authentication in its chain and may perform " + plural(len(ws), "write") + ": " + strings.Join(firstN(labels, 3), "; ") + "."
		f.ev(Evidence{Kind: EvRegistration, Text: "registered as " + rt.Key, File: rt.File, Line: rt.Line, Source: "go/ssa"})
		note := "no element of the chain reads a credential and answers 401"
		if rt.Auth != nil && rt.Auth.Note != "" {
			note += " (" + rt.Auth.Note + ")"
		}
		f.ev(Evidence{Kind: EvAbsence, Text: note, Source: "httpapi"})
		for _, w := range firstWrites(ws, 5) {
			f.ev(Evidence{Kind: EvSink, Text: "may perform " + w.label, File: w.file, Line: w.line, Symbol: w.id, Source: "go/ssa"})
		}
		f.Routes[0].Path = s.pathTo(rt, rc, ws[0].id)
		f.limit("authentication done outside the handler chain (gateway, proxy, mTLS) is not visible")
		s.add(f)
	}
}

func plural(n int, w string) string {
	if n == 1 {
		return "1 " + w
	}
	return itoa(n) + " " + w + "s"
}

func firstN(xs []string, n int) []string {
	if len(xs) > n {
		return append(append([]string(nil), xs[:n]...), "…")
	}
	return xs
}

func firstWrites(xs []write, n int) []write {
	if len(xs) > n {
		return xs[:n]
	}
	return xs
}

// ---------- GO-AUTHN-002 ----------

func ruleCookies(s *scanner, r *rule) {
	// gin's SetCookie(name, value, maxAge, path, domain, secure, httpOnly)
	s.eachCall(func(c callInfo) {
		if c.name != "(*github.com/gin-gonic/gin.Context).SetCookie" || len(c.args) < 7 {
			return
		}
		// Only constant false arguments are reported; computed ones are
		// unknown to the analysis.
		var missing []string
		for _, a := range []struct {
			i    int
			attr string
		}{{5, "Secure"}, {6, "HttpOnly"}} {
			if v, ok := constBool(c.args[a.i]); ok && !v {
				missing = append(missing, a.attr)
			}
		}
		if len(missing) == 0 {
			return
		}
		name, _ := constString(c.args[0])
		s.cookieFinding(r, c.p, c.fn, c.cs.File, c.cs.Line, c.text(), name, missing, ConfConfirmed, "gin SetCookie")
	})
	// http.Cookie literals
	s.eachFile(func(p *flow.Program, pkg *packages.Package, file *ast.File, rel string) {
		var stack []ast.Node
		ast.Inspect(file, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			lit, ok := n.(*ast.CompositeLit)
			if !ok || typeName(pkg.TypesInfo, lit) != "net/http.Cookie" {
				return true
			}
			set := map[string]ast.Expr{}
			name := ""
			for _, el := range lit.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					if id, ok := kv.Key.(*ast.Ident); ok {
						set[id.Name] = kv.Value
						if id.Name == "Name" {
							if tv, ok := pkg.TypesInfo.Types[kv.Value]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
								name = constant.StringVal(tv.Value)
							}
						}
					}
				}
			}
			assigned := assignedFields(pkg.TypesInfo, stack, lit)
			var missing []string
			for _, attr := range []string{"Secure", "HttpOnly"} {
				if assigned[attr] {
					continue
				}
				e, ok := set[attr]
				if !ok {
					missing = append(missing, attr)
					continue
				}
				if tv, ok := pkg.TypesInfo.Types[e]; ok && tv.Value != nil && tv.Value.Kind() == constant.Bool && !constant.BoolVal(tv.Value) {
					missing = append(missing, attr)
				}
			}
			if len(missing) == 0 {
				return true
			}
			if _, ok := set["SameSite"]; !ok && !assigned["SameSite"] {
				missing = append(missing, "SameSite")
			}
			pos := p.Fset().Position(lit.Pos())
			line := pos.Line
			fn := p.FuncAt(rel, line)
			s.cookieFinding(r, p, fn, rel, line, p.NodeText(lit), name, missing, ConfConfirmed, "http.Cookie")
			return true
		})
	})
}

// assignedFields lists fields of the cookie set after the literal, when it
// is stored in a variable (ck := &http.Cookie{…}; ck.Secure = true).
func assignedFields(info *types.Info, stack []ast.Node, lit *ast.CompositeLit) map[string]bool {
	out := map[string]bool{}
	var obj types.Object
	var body *ast.BlockStmt
	for k := len(stack) - 1; k >= 0; k-- {
		switch x := stack[k].(type) {
		case *ast.AssignStmt:
			if obj == nil && len(x.Lhs) == 1 {
				if id, ok := x.Lhs[0].(*ast.Ident); ok {
					obj = info.ObjectOf(id)
				}
			}
		case *ast.ValueSpec:
			if obj == nil && len(x.Names) == 1 {
				obj = info.ObjectOf(x.Names[0])
			}
		case *ast.FuncDecl:
			body = x.Body
		case *ast.FuncLit:
			if body == nil {
				body = x.Body
			}
		}
		if body != nil {
			break
		}
	}
	if obj == nil || body == nil {
		return out
	}
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, l := range as.Lhs {
			if sel, ok := l.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && info.ObjectOf(id) == obj {
					out[sel.Sel.Name] = true
				}
			}
		}
		return true
	})
	return out
}

func (s *scanner) cookieFinding(r *rule, p *flow.Program, fn *ssa.Function, file string, line int, text, name string, missing []string, conf, how string) {
	f := s.newFinding(r, p, fn, file, line, text)
	f.Confidence = conf
	f.Verdict = Review
	label := "a cookie"
	if name != "" {
		label = "cookie " + `"` + name + `"`
	}
	f.Message = label + " is set without " + strings.Join(missing, ", ") + "."
	if conf == ConfConfirmed && (contains(missing, "Secure") && contains(missing, "HttpOnly")) {
		f.Verdict = Vulnerable
	}
	f.ev(Evidence{Kind: EvConfig, Text: how + " without " + strings.Join(missing, ", "), File: file, Line: line, Code: code(p, file, line), Symbol: f.SymbolID, Confidence: evidence.Exact, Source: "go/types"})
	s.linkRoutes(f)
	s.add(f)
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// ---------- GO-AUTHN-003 ----------

var jwtPkgs = []string{"github.com/golang-jwt/jwt/v5", "github.com/golang-jwt/jwt/v4", "github.com/golang-jwt/jwt", "github.com/dgrijalva/jwt-go"}

func isJWTPkg(path string) bool {
	for _, p := range jwtPkgs {
		if path == p {
			return true
		}
	}
	return false
}

func ruleJWT(s *scanner, r *rule) {
	s.eachCall(func(c callInfo) {
		for _, pk := range jwtPkgs {
			if c.name == "(*"+pk+".Parser).ParseUnverified" {
				f := s.newFinding(r, c.p, c.fn, c.cs.File, c.cs.Line, c.text())
				f.Confidence, f.Verdict = ConfLikely, Review
				f.Message = "The token is parsed with ParseUnverified: its signature is not checked. Review whether its claims are trusted afterwards."
				f.ev(Evidence{Kind: EvSink, Text: "ParseUnverified", File: c.cs.File, Line: c.cs.Line, Code: code(c.p, c.cs.File, c.cs.Line), Symbol: f.SymbolID, Confidence: evidence.Exact, Source: "go/ssa"})
				s.linkRoutes(f)
				s.add(f)
				return
			}
		}
	})
	s.eachFile(func(p *flow.Program, pkg *packages.Package, file *ast.File, rel string) {
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "SigningMethodNone" && sel.Sel.Name != "UnsafeAllowNoneSignatureType") {
				return true
			}
			obj := pkg.TypesInfo.ObjectOf(sel.Sel)
			if obj == nil || obj.Pkg() == nil || !isJWTPkg(obj.Pkg().Path()) {
				return true
			}
			line := p.Fset().Position(sel.Pos()).Line
			f := s.newFinding(r, p, p.FuncAt(rel, line), rel, line, p.NodeText(sel))
			f.Confidence, f.Verdict, f.Severity = ConfConfirmed, Vulnerable, SevHigh
			f.Message = "The code refers to " + sel.Sel.Name + ": tokens with alg=none (no signature) can be accepted."
			f.ev(Evidence{Kind: EvConfig, Text: "use of jwt." + sel.Sel.Name, File: rel, Line: line, Code: code(p, rel, line), Symbol: f.SymbolID, Confidence: evidence.Exact, Source: "go/types"})
			s.linkRoutes(f)
			s.add(f)
			return true
		})
	})
}
