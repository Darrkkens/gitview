package audit

import (
	"go/ast"
	"go/constant"
	"go/token"
	"regexp"
	"strings"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
	"github.com/athos/gitview/internal/graph/model"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
)

// ---------- redaction ----------

// secretFormats are credential formats recognisable by their shape.
var secretFormats = []struct {
	name string
	re   *regexp.Regexp
}{
	{"private key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"AWS access key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"GitHub token", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{60,})`)},
	{"Slack token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	{"Stripe key", regexp.MustCompile(`\b(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{16,}`)},
	{"API secret key", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`)},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{"JSON Web Token", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
}

var (
	assignedSecret = regexp.MustCompile(`(?i)((?:passw(?:or)?d|passwd|pwd|secret|api[_-]?key|access[_-]?token|auth[_-]?token|client[_-]?secret|private[_-]?key)[A-Za-z0-9_]*["']?\s*[:=]+\s*)(["` + "`" + `'])([^"'` + "`" + `\s]{6,})(["` + "`" + `'])`)
	dsnPassword    = regexp.MustCompile(`(?i)\b((?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis|amqps?)://[^:/\s@]+:)([^@\s]+)(@)`)
)

const redacted = "«redacted»"

// Redact removes credentials from text that leaves the analysis (reports,
// evidence, the AI context): recognisable formats, values assigned to
// secret-like names and passwords in connection strings.
func Redact(s string) string {
	if s == "" {
		return s
	}
	for _, f := range secretFormats {
		s = f.re.ReplaceAllString(s, redacted)
	}
	s = assignedSecret.ReplaceAllString(s, "${1}${2}"+redacted+"${4}")
	s = dsnPassword.ReplaceAllString(s, "${1}"+redacted+"${3}")
	return s
}

var anyString = regexp.MustCompile("\"(?:[^\"\\\\]|\\\\.)*\"|`[^`]*`")

// RedactStrings replaces every string literal of a line: used for the
// evidence of a hardcoded secret, whose value must never be shown.
func RedactStrings(s string) string { return anyString.ReplaceAllString(s, `"`+redacted+`"`) }

// ---------- GO-DATA-001 ----------

var (
	secretName  = regexp.MustCompile(`(?i)(passw(?:or)?d|passwd|pwd$|secret|token|api_?key|private_?key|client_?secret|access_?key|signing_?key|credential|auth_?key)`)
	notASecret  = regexp.MustCompile(`(?i)(name|field|header|env|path|file|url|uri|prefix|suffix|type|kind|label|param|query|column|key_?id|length|len|ttl|expir|duration|regex|pattern|format|cookie_?name|template)$`)
	placeholder = regexp.MustCompile(`\{\{|\$\{|<[^>]*>|%[sdvq]|^x+$|^\*+$|^\.\.\.|example|placeholder|your[_-]`)
)

// looksLikeName: values that name something (a field, a header, an
// environment variable) rather than hold a secret.
func looksLikeName(v string) bool {
	if regexp.MustCompile(`^[A-Z0-9_]+$`).MatchString(v) {
		return true // environment variable name
	}
	return len(v) < 32 && regexp.MustCompile(`^[A-Za-z_.\-]+$`).MatchString(v)
}

func ruleHardcodedSecrets(s *scanner, r *rule) {
	s.eachFile(func(p *flow.Program, pkg *packages.Package, file *ast.File, rel string) {
		info := pkg.TypesInfo
		constVal := func(e ast.Expr) (string, bool) {
			if tv, ok := info.Types[e]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
				return constant.StringVal(tv.Value), true
			}
			return "", false
		}
		report := func(pos token.Pos, name, value, format string) {
			line := p.Fset().Position(pos).Line
			text := name
			if text == "" {
				text = format
			}
			f := s.newFinding(r, p, p.FuncAt(rel, line), rel, line, rel+":"+text)
			f.File, f.Line = rel, line
			src := RedactStrings(strings.TrimSpace(p.Source(rel, line, line)))
			if format != "" {
				f.Confidence, f.Verdict = ConfConfirmed, Vulnerable
				f.Message = "A constant string has the format of a " + format + "."
				f.ev(Evidence{Kind: EvConfig, Text: format + " in a string constant (value withheld)", File: rel, Line: line, Code: src, Confidence: evidence.Exact, Source: "go/types"})
			} else {
				f.Confidence, f.Verdict = ConfLikely, Review
				f.Message = name + " is assigned a constant string of " + itoa(len(value)) + " characters."
				f.ev(Evidence{Kind: EvConfig, Text: name + " holds a constant (value withheld)", Basis: "heuristic", File: rel, Line: line, Code: src, Confidence: evidence.Exact, Source: "go/types"})
			}
			f.Evidence[len(f.Evidence)-1].Code = src // already free of the value
			s.add(f)
		}
		reported := map[token.Pos]bool{}
		named := func(name string, e ast.Expr) {
			if !literalExpr(e) {
				return
			}
			v, ok := constVal(e)
			if !ok || reported[e.Pos()] || !secretName.MatchString(name) || notASecret.MatchString(name) {
				return
			}
			// Text with spaces describes a credential ("Authorization
			// header") rather than holding one; known formats are still
			// reported below by format.
			if len(v) < 8 || looksLikeName(v) || placeholder.MatchString(v) || strings.ContainsAny(v, " \t\n") {
				return
			}
			for _, f := range secretFormats {
				if f.re.MatchString(v) {
					return // reported by format
				}
			}
			reported[e.Pos()] = true
			report(e.Pos(), name, v, "")
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.ValueSpec:
				for i, id := range x.Names {
					if i < len(x.Values) {
						named(id.Name, x.Values[i])
					}
				}
			case *ast.AssignStmt:
				for i, l := range x.Lhs {
					if i >= len(x.Rhs) {
						break
					}
					switch lh := l.(type) {
					case *ast.Ident:
						named(lh.Name, x.Rhs[i])
					case *ast.SelectorExpr:
						named(lh.Sel.Name, x.Rhs[i])
					}
				}
			case *ast.KeyValueExpr:
				if id, ok := x.Key.(*ast.Ident); ok {
					named(id.Name, x.Value)
				} else if k, ok := constVal(x.Key); ok {
					named(k, x.Value)
				}
			case *ast.BasicLit, *ast.BinaryExpr:
				e := x.(ast.Expr)
				if v, ok := constVal(e); ok && !reported[e.Pos()] {
					for _, f := range secretFormats {
						if f.re.MatchString(v) {
							reported[e.Pos()] = true
							report(e.Pos(), "", v, f.name)
							return false
						}
					}
					if _, isLit := e.(*ast.BasicLit); !isLit {
						return false // a folded constant: its parts are part of it
					}
				}
			}
			return true
		})
	})
}

// literalExpr: a constant written in place (a literal, or literals joined
// by operators), not a reference to a constant declared elsewhere.
func literalExpr(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.BasicLit:
		return true
	case *ast.BinaryExpr:
		return literalExpr(x.X) && literalExpr(x.Y)
	case *ast.ParenExpr:
		return literalExpr(x.X)
	}
	return false
}

// ---------- GO-DATA-002 ----------

// responders write a response body: the index of the body argument.
var responders = map[string]int{
	"(*github.com/gin-gonic/gin.Context).JSON": 1, "(*github.com/gin-gonic/gin.Context).AbortWithStatusJSON": 1,
	"(*github.com/gin-gonic/gin.Context).IndentedJSON": 1, "(*github.com/gin-gonic/gin.Context).String": 2,
	"net/http.Error": 1, "(github.com/labstack/echo/v4.Context).JSON": 1, "(github.com/labstack/echo/v4.Context).String": 1,
}

// errorText reports whether v is err.Error() (directly, in a map or in
// the variadic arguments of a formatter).
func errorText(v ssa.Value, depth int) bool {
	if depth > 3 || v == nil {
		return false
	}
	switch x := v.(type) {
	case *ssa.MakeInterface:
		return errorText(x.X, depth+1)
	case *ssa.Call:
		c := x.Common()
		if c.IsInvoke() && c.Method.Name() == "Error" && len(c.Args) == 0 {
			return true
		}
		if fn := c.StaticCallee(); fn != nil && fn.Name() == "Error" && fn.Signature.Recv() != nil && len(c.Args) == 1 {
			return true
		}
	case *ssa.MakeMap:
		for _, ref := range *x.Referrers() {
			if mu, ok := ref.(*ssa.MapUpdate); ok && errorText(mu.Value, depth+1) {
				return true
			}
		}
	case *ssa.Slice:
		if elems, ok := flow.VariadicElems(x); ok {
			for _, e := range elems {
				if errorText(e, depth+1) {
					return true
				}
			}
		}
	}
	return false
}

func statusOf(v ssa.Value) int {
	if k, ok := v.(*ssa.Const); ok && k.Value != nil && k.Value.Kind() == constant.Int {
		n, _ := constant.Int64Val(k.Value)
		return int(n)
	}
	return 0
}

func ruleErrorDetails(s *scanner, r *rule) {
	s.eachCall(func(c callInfo) {
		bi, ok := responders[c.name]
		if !ok || bi >= len(c.args) {
			return
		}
		if !errorText(c.args[bi], 0) {
			return
		}
		status := 0
		switch c.name {
		case "net/http.Error":
			if len(c.args) > 2 {
				status = statusOf(c.args[2])
			}
		default:
			status = statusOf(c.args[0])
		}
		if status != 0 && status < 500 {
			return // client errors usually describe the request on purpose
		}
		f := s.newFinding(r, c.p, c.fn, c.cs.File, c.cs.Line, c.text())
		f.Confidence, f.Verdict = ConfLikely, Review
		if status == 0 {
			f.Confidence = ConfPossible
			f.Message = "The response body includes err.Error(); its status is computed at runtime."
		} else {
			f.Message = "The " + itoa(status) + " response body includes err.Error()."
		}
		f.ev(Evidence{Kind: EvSink, Text: shortCall(c.name) + " with err.Error() in the body", File: c.cs.File, Line: c.cs.Line, Code: code(c.p, c.cs.File, c.cs.Line), Symbol: f.SymbolID, Confidence: evidence.Exact, Source: "go/ssa"})
		s.linkRoutes(f)
		s.add(f)
	})
}

// ---------- GO-DATA-003 ----------

var sensitiveField = regexp.MustCompile(`(?i)(passw(?:or)?d|passwd|pwd_?hash|secret|api_?key|private_?key|salt|otp_?seed|totp_?secret)`)

func ruleSensitiveResponse(s *scanner, r *rule) {
	if s.g.HTTP == nil {
		return
	}
	defs := s.g.HTTP.Schemas
	type hit struct {
		path  string
		field model.Field
	}
	var walk func(sh *model.Shape, prefix string, depth int, seen map[string]bool, out *[]hit)
	walk = func(sh *model.Shape, prefix string, depth int, seen map[string]bool, out *[]hit) {
		if sh == nil || depth > 5 {
			return
		}
		if sh.Ref != "" {
			if seen[sh.Ref] {
				return
			}
			seen = map[string]bool{sh.Ref: true}
			if d := defs[sh.Ref]; d != nil {
				sh = d
			}
		}
		if sh.Items != nil {
			walk(sh.Items, prefix+"[]", depth+1, seen, out)
		}
		for _, f := range sh.Fields {
			p := f.Name
			if prefix != "" {
				p = prefix + "." + f.Name
			}
			if sensitiveField.MatchString(f.Name) || sensitiveField.MatchString(f.GoName) {
				*out = append(*out, hit{p, f})
			}
			walk(f.Schema, p, depth+1, seen, out)
		}
	}
	// One finding per response (handler and the call that writes it),
	// with every route that answers through it.
	type group struct {
		resp   model.Response
		hits   []hit
		routes []*model.Route
	}
	groups := map[string]*group{}
	var order []string
	for _, rt := range s.sortedRoutes() {
		for _, resp := range rt.Responses {
			if resp.Status != 0 && (resp.Status < 200 || resp.Status >= 300) {
				continue
			}
			var hits []hit
			walk(resp.Schema, "", 0, map[string]bool{}, &hits)
			if len(hits) == 0 {
				continue
			}
			loc := ""
			if len(resp.Evidence) > 0 {
				loc = resp.Evidence[0].File + ":" + itoa(resp.Evidence[0].Line)
			}
			key := resp.From + "|" + resp.Source + "|" + loc + "|" + hits[0].path
			g := groups[key]
			if g == nil {
				g = &group{resp: resp, hits: hits}
				groups[key] = g
				order = append(order, key)
			}
			g.routes = append(g.routes, rt)
		}
	}
	for _, key := range order {
		g := groups[key]
		resp := g.resp
		f := s.routeFinding(r, g.routes[0])
		f.ID = stableID("finding:"+r.Category+":", r.ID, resp.From, resp.Source, g.hits[0].path)
		f.Anchor = r.ID + "|" + resp.From + "|" + resp.Source + "|" + g.hits[0].path
		for _, rt := range g.routes[1:] {
			f.Routes = append(f.Routes, s.routeFinding(r, rt).Routes...)
		}
		f.Confidence, f.Verdict = ConfLikely, Review
		var names, keys []string
		for _, h := range g.hits {
			names = append(names, h.path)
		}
		for _, rt := range g.routes {
			keys = append(keys, rt.Key)
		}
		f.Message = "The " + statusText(resp.Status) + " response written by " + resp.From + " (" + strings.Join(keys, ", ") + ") has " + strings.Join(names, ", ") + "."
		for _, h := range g.hits {
			f.ev(Evidence{Kind: EvSink, Text: "field " + h.path + " (Go field " + h.field.GoName + ") in the response body", Basis: "heuristic", File: h.field.File, Line: h.field.Line, Source: "httpapi"})
		}
		for _, e := range resp.Evidence {
			f.ev(Evidence{Kind: EvSink, Text: "response written by " + resp.Source + " in " + resp.From, File: e.File, Line: e.Line, Code: code(s.progOf(e.File), e.File, e.Line), Source: "httpapi"})
			f.File, f.Line = e.File, e.Line // the finding is where the response is written
			break
		}
		if resp.Schema != nil && resp.Schema.Partial {
			f.limit("the response schema is partial: " + resp.Schema.Note)
		}
		s.add(f)
	}
}

func statusText(n int) string {
	if n == 0 {
		return "success"
	}
	return itoa(n)
}

// ---------- GO-DATA-004 ----------

var loggers = map[string]bool{
	"log.Print": true, "log.Printf": true, "log.Println": true, "log.Fatal": true, "log.Fatalf": true, "log.Fatalln": true,
	"log.Panic": true, "log.Panicf": true, "log.Panicln": true,
	"(*log.Logger).Print": true, "(*log.Logger).Printf": true, "(*log.Logger).Println": true,
	"log/slog.Info": true, "log/slog.Debug": true, "log/slog.Warn": true, "log/slog.Error": true,
	"(*log/slog.Logger).Info": true, "(*log/slog.Logger).Debug": true, "(*log/slog.Logger).Warn": true, "(*log/slog.Logger).Error": true,
	"fmt.Print": true, "fmt.Printf": true, "fmt.Println": true,
}

var credentialLabel = regexp.MustCompile(`(?i)authorization|cookie|passw|token|secret|api[_-]?key`)

func ruleCredentialLogs(s *scanner, r *rule) {
	s.eachCall(func(c callInfo) {
		if !loggers[c.name] {
			return
		}
		var vals []ssa.Value
		for _, a := range c.args {
			if elems, ok := flow.VariadicElems(a); ok {
				vals = append(vals, elems...)
			} else {
				vals = append(vals, a)
			}
		}
		for _, a := range vals {
			if mi, ok := a.(*ssa.MakeInterface); ok {
				a = mi.X
			}
			if _, isConst := a.(*ssa.Const); isConst {
				continue
			}
			t := s.trace(c.p, c.fn, a, "a logged value")
			for _, path := range t.untrusted {
				matched := ""
				for _, st := range path.steps {
					if credentialLabel.MatchString(st.Label) {
						matched = st.Label
						break
					}
				}
				if matched == "" {
					continue
				}
				f := s.newFinding(r, c.p, c.fn, c.cs.File, c.cs.Line, c.text())
				f.Confidence, f.Verdict = findingConfidence([]taintPath{path}), Review
				if f.Confidence == ConfConfirmed {
					f.Verdict = Vulnerable
				}
				f.Message = shortCall(c.name) + " writes a value that comes from " + path.steps[0].Label + " (" + matched + ")."
				s.addFlow(f, t, []taintPath{path})
				f.ev(Evidence{Kind: EvSink, Text: "logged by " + shortCall(c.name), File: c.cs.File, Line: c.cs.Line, Code: code(c.p, c.cs.File, c.cs.Line), Symbol: f.SymbolID, Confidence: evidence.Exact, Source: "go/ssa"})
				f.Flow = append(f.Flow, FlowStep{Role: "sink", Label: "logged by " + shortCall(c.name), File: c.cs.File, Line: c.cs.Line, Code: code(c.p, c.cs.File, c.cs.Line)})
				s.linkRoutes(f)
				s.add(f)
				return
			}
		}
	})
}
