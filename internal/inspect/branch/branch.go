// Package branch is the Branch Explorer: the control-flow graph (CFG) of one
// Go function, built from its syntax with golang.org/x/tools/go/cfg.
//
// The graph is structural. A path through it is a path the code allows,
// not one that was executed, and some paths may be infeasible because their
// conditions contradict each other. Coverage, when supplied, is overlaid per
// block and keeps the granularity of Go's coverage counters.
package branch

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/athos/gitview/internal/evidence"
	"github.com/athos/gitview/internal/flow"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/cfg"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/types/typeutil"
)

// Decision kinds.
const (
	DecIf         = "if"
	DecFor        = "for"
	DecRange      = "range"
	DecSwitch     = "switch"
	DecTypeSwitch = "type switch"
	DecSelect     = "select"
)

// Terminal kinds: how control leaves the function from a block.
const (
	TermReturn   = "return"
	TermImplicit = "implicit return" // falls off the end of the body
	TermPanic    = "panic"
	TermExit     = "exit"   // os.Exit, log.Fatal*, testing Fatal/FailNow/Skip
	TermGoexit   = "goexit" // runtime.Goexit
	TermBlocked  = "blocks forever"
)

// Edge kinds.
const (
	EdgeBranch = "branch" // outcome of a decision
	EdgeJump   = "jump"   // sequential flow or break/continue/goto
	EdgeLoop   = "loop"   // back edge closing a loop
)

type Stmt struct {
	Line int    `json:"line"`
	Text string `json:"text"`
	Kind string `json:"kind"` // stmt | assign | return | defer | go | send | var | panic | exit | goexit
}

type Block struct {
	ID       int    `json:"id"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	EndLine  int    `json:"end"`
	Stmts    []Stmt `json:"stmts,omitempty"`
	Decision string `json:"decision,omitempty"` // if | for | range | switch | type switch | select
	Cond     string `json:"cond,omitempty"`     // condition, switch tag or range expression
	CondLine int    `json:"condLine,omitempty"`
	Terminal string `json:"terminal,omitempty"`
	Entry    bool   `json:"entry,omitempty"`

	Reachable    bool `json:"reachable"`
	LoopHead     bool `json:"loopHead,omitempty"`
	ErrorCheck   bool `json:"errorCheck,omitempty"`   // decision tests an error value
	ReturnsError bool `json:"returnsError,omitempty"` // return whose error result is not nil
	Defers       int  `json:"defers,omitempty"`

	Coverage   string `json:"coverage,omitempty"` // covered | uncovered | partial | unknown
	CoverCount int64  `json:"coverCount,omitempty"`

	raw   *cfg.Block
	nodes []ast.Node
	succs []*Edge
}

type Edge struct {
	From  int    `json:"from"`
	To    int    `json:"to"`
	Label string `json:"label,omitempty"`
	Kind  string `json:"kind"`
	Error bool   `json:"error,omitempty"` // the branch taken when an error is present

	to *Block
}

// Path is one route from the entry to a terminal block in which each loop
// back edge is taken at most once.
type Path struct {
	Blocks []int    `json:"blocks"`
	Edges  []int    `json:"edges"` // indices into Graph.Edges
	Steps  []string `json:"steps"` // readable decisions taken, e.g. "id <= 0 → true"
	End    int      `json:"end"`   // terminal block
	Exit   string   `json:"exit"`  // terminal kind
	Error  bool     `json:"error,omitempty"`
}

type Metrics struct {
	Blocks         int  `json:"blocks"`
	Edges          int  `json:"edges"`
	DecisionPoints int  `json:"decisionPoints"`
	Decisions      int  `json:"decisions"`  // outcomes beyond the first, summed over decision points
	Cyclomatic     int  `json:"cyclomatic"` // decisions + 1
	Returns        int  `json:"returns"`
	Panics         int  `json:"panics"` // panic, exit and goexit terminals
	Loops          int  `json:"loops"`
	Unreachable    int  `json:"unreachable"`
	Defers         int  `json:"defers"`
	Paths          int  `json:"paths"` // entry→exit paths, each loop repeated at most once (saturates at MaxCount)
	PathsListed    int  `json:"pathsListed"`
	PathsSaturated bool `json:"pathsSaturated,omitempty"`
}

// MaxCount caps path enumeration.
const MaxCount = 10_000

type Graph struct {
	Symbol    evidence.SymbolRef    `json:"symbol"`
	Blocks    []*Block              `json:"blocks"`
	Edges     []*Edge               `json:"edges"`
	Paths     []Path                `json:"paths"`
	Metrics   Metrics               `json:"metrics"`
	Defers    []Stmt                `json:"defers,omitempty"`
	Coverage  *CoverageSummary      `json:"coverage,omitempty"`
	Status    evidence.Completeness `json:"status"`
	Notes     []string              `json:"notes,omitempty"`
	Evidence  []evidence.Evidence   `json:"evidence"`
	EntryID   int                   `json:"entry"`
	Truncated bool                  `json:"truncated,omitempty"`
}

type Options struct {
	MaxPaths int      // paths listed (default 200)
	Coverage *Profile // optional coverage overlay
}

// Block returns the block with the given id.
func (g *Graph) Block(id int) *Block {
	if id >= 0 && id < len(g.Blocks) && g.Blocks[id].ID == id {
		return g.Blocks[id]
	}
	for _, b := range g.Blocks {
		if b.ID == id {
			return b
		}
	}
	return nil
}

// Out returns the edges leaving block id, in order.
func (g *Graph) Out(id int) []*Edge {
	var out []*Edge
	for _, e := range g.Edges {
		if e.From == id {
			out = append(out, e)
		}
	}
	return out
}

type builder struct {
	p    *flow.Program
	fn   *ssa.Function
	info *types.Info
	body *ast.BlockStmt
	raw  *cfg.CFG

	terms     map[*ast.CallExpr]string // non-returning calls → terminal kind
	nodeBlock map[ast.Node]*cfg.Block
	removed   map[ast.Node]bool
	absorbed  map[int32]bool
	jumpLabel map[int32]string
	switchOf  map[*ast.CaseClause]ast.Stmt
	selectOf  map[*ast.CommClause]*ast.SelectStmt
	parent    map[ast.Stmt]stmtPos

	blocks map[int32]*Block
}

type stmtPos struct {
	owner ast.Node // *ast.BlockStmt, *ast.CaseClause, *ast.CommClause or *ast.LabeledStmt
	list  []ast.Stmt
	idx   int
}

// Build constructs the control-flow graph of fn.
func Build(p *flow.Program, fn *ssa.Function, opt Options) (*Graph, error) {
	fn = flow.Origin(fn)
	body, _ := flow.SyntaxOf(fn)
	if body == nil {
		return nil, fmt.Errorf("%s has no Go body to analyse", flow.ShortName(fn))
	}
	info := p.TypesInfo(fn)
	if info == nil {
		return nil, fmt.Errorf("no type information for %s", flow.ShortName(fn))
	}
	if opt.MaxPaths <= 0 {
		opt.MaxPaths = 200
	}
	b := &builder{p: p, fn: fn, info: info, body: body,
		terms: map[*ast.CallExpr]string{}, nodeBlock: map[ast.Node]*cfg.Block{}, removed: map[ast.Node]bool{},
		absorbed: map[int32]bool{}, jumpLabel: map[int32]string{}, switchOf: map[*ast.CaseClause]ast.Stmt{},
		selectOf: map[*ast.CommClause]*ast.SelectStmt{}, parent: map[ast.Stmt]stmtPos{}, blocks: map[int32]*Block{}}
	b.raw = cfg.New(body, b.mayReturn)
	b.indexAST()
	for _, rb := range b.raw.Blocks {
		for _, n := range rb.Nodes {
			b.nodeBlock[n] = rb
		}
	}
	b.labelJumps()

	g := &Graph{Symbol: p.Ref(fn)}
	b.convert()
	b.finish(g, opt)
	if opt.Coverage != nil {
		opt.Coverage.Overlay(p, g)
	}
	g.Evidence = append(g.Evidence, evidence.Evidence{Kind: evidence.Static, Basis: evidence.BasisStatic,
		File: g.Symbol.File, Line: g.Symbol.Line, SymbolID: g.Symbol.ID, Source: "golang.org/x/tools/go/cfg",
		Details: "control-flow graph built from the function's syntax and type information"})
	g.Status.Done()
	return g, nil
}

// noReturn lists functions documented never to return to their caller.
var noReturn = map[string]string{
	"os.Exit": TermExit, "log.Fatal": TermExit, "log.Fatalf": TermExit, "log.Fatalln": TermExit,
	"log.Panic": TermPanic, "log.Panicf": TermPanic, "log.Panicln": TermPanic,
	"(*log.Logger).Fatal": TermExit, "(*log.Logger).Fatalf": TermExit, "(*log.Logger).Fatalln": TermExit,
	"(*log.Logger).Panic": TermPanic, "(*log.Logger).Panicf": TermPanic, "(*log.Logger).Panicln": TermPanic,
	"runtime.Goexit":          TermGoexit,
	"(*testing.common).Fatal": TermExit, "(*testing.common).Fatalf": TermExit, "(*testing.common).FailNow": TermExit,
	"(*testing.common).Skip": TermExit, "(*testing.common).Skipf": TermExit, "(*testing.common).SkipNow": TermExit,
}

func (b *builder) mayReturn(call *ast.CallExpr) bool {
	if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
		if bi, ok := b.info.Uses[id].(*types.Builtin); ok && bi.Name() == "panic" {
			b.terms[call] = TermPanic
			return false
		}
	}
	if f, ok := typeutil.Callee(b.info, call).(*types.Func); ok {
		if t, ok := noReturn[f.FullName()]; ok {
			b.terms[call] = t
			return false
		}
	}
	return true
}

// indexAST records the parent statement list of every statement and the
// switch or select owning each clause. Nested function literals have their
// own graphs and are skipped.
func (b *builder) indexAST() {
	var visitList func(owner ast.Node, list []ast.Stmt)
	var visit func(s ast.Stmt)
	visitList = func(owner ast.Node, list []ast.Stmt) {
		for i, s := range list {
			b.parent[s] = stmtPos{owner, list, i}
			visit(s)
		}
	}
	visit = func(s ast.Stmt) {
		switch s := s.(type) {
		case *ast.BlockStmt:
			visitList(s, s.List)
		case *ast.LabeledStmt:
			b.parent[s.Stmt] = stmtPos{owner: s, idx: 0}
			visit(s.Stmt)
		case *ast.IfStmt:
			visitList(s.Body, s.Body.List)
			if s.Else != nil {
				if eb, ok := s.Else.(*ast.BlockStmt); ok {
					visitList(eb, eb.List)
				} else {
					visit(s.Else)
				}
			}
		case *ast.ForStmt:
			visitList(s.Body, s.Body.List)
		case *ast.RangeStmt:
			visitList(s.Body, s.Body.List)
		case *ast.SwitchStmt:
			for _, c := range s.Body.List {
				cc := c.(*ast.CaseClause)
				b.switchOf[cc] = s
				visitList(cc, cc.Body)
			}
		case *ast.TypeSwitchStmt:
			for _, c := range s.Body.List {
				cc := c.(*ast.CaseClause)
				b.switchOf[cc] = s
				visitList(cc, cc.Body)
			}
		case *ast.SelectStmt:
			for _, c := range s.Body.List {
				cc := c.(*ast.CommClause)
				b.selectOf[cc] = s
				visitList(cc, cc.Body)
			}
		}
	}
	visitList(b.body, b.body.List)
}

func (b *builder) findBlock(kind cfg.BlockKind, stmt ast.Node) *cfg.Block {
	for _, rb := range b.raw.Blocks {
		if rb.Kind == kind && rb.Stmt == stmt {
			return rb
		}
	}
	return nil
}

// blockBefore returns the block that is current when s starts executing,
// following the construction rules of go/cfg.
func (b *builder) blockBefore(s ast.Stmt) *cfg.Block {
	pos, ok := b.parent[s]
	if !ok {
		return nil
	}
	if pos.idx > 0 {
		return b.blockAfter(pos.list[pos.idx-1])
	}
	return b.startBlock(pos.owner)
}

func (b *builder) startBlock(owner ast.Node) *cfg.Block {
	if owner == b.body {
		return b.raw.Blocks[0]
	}
	if ls, ok := owner.(*ast.LabeledStmt); ok {
		return b.findBlock(cfg.KindLabel, ls)
	}
	// Find the construct that owns this list.
	var found *cfg.Block
	ast.Inspect(b.body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		switch s := n.(type) {
		case *ast.IfStmt:
			if s.Body == owner {
				found = b.findBlock(cfg.KindIfThen, s)
			} else if s.Else == owner {
				found = b.findBlock(cfg.KindIfElse, s)
			}
		case *ast.ForStmt:
			if s.Body == owner {
				found = b.findBlock(cfg.KindForBody, s)
			}
		case *ast.RangeStmt:
			if s.Body == owner {
				found = b.findBlock(cfg.KindRangeBody, s)
			}
		case *ast.CaseClause:
			if s == owner {
				if s.List != nil {
					found = b.findBlock(cfg.KindSwitchCaseBody, s)
				} else if sw, ok := b.switchOf[s].(*ast.TypeSwitchStmt); ok {
					found = b.lastChainBlock(sw.Body.List, cfg.KindSwitchNextCase)
					if found == nil {
						found = b.nodeBlock[sw.Assign]
					}
				} else {
					found = b.findBlock(cfg.KindSwitchCaseBody, s)
				}
			}
		case *ast.CommClause:
			if s == owner {
				if s.Comm != nil {
					found = b.findBlock(cfg.KindSelectCaseBody, s)
				} else {
					sel := b.selectOf[s]
					found = b.lastChainBlock(sel.Body.List, cfg.KindSelectAfterCase)
					if found == nil {
						found = b.blockBefore(sel)
					}
				}
			}
		case *ast.BlockStmt:
			if s == owner && s != b.body {
				// A plain nested block: it continues the enclosing block.
				if _, isStmt := b.parent[s]; isStmt {
					found = b.blockBefore(s)
				}
			}
		}
		return true
	})
	return found
}

// lastChainBlock returns the chain block of the highest index whose Stmt is
// one of the clauses.
func (b *builder) lastChainBlock(clauses []ast.Stmt, kind cfg.BlockKind) *cfg.Block {
	var best *cfg.Block
	for _, rb := range b.raw.Blocks {
		if rb.Kind != kind {
			continue
		}
		for _, c := range clauses {
			if rb.Stmt == c && (best == nil || rb.Index > best.Index) {
				best = rb
			}
		}
	}
	return best
}

func (b *builder) blockAfter(s ast.Stmt) *cfg.Block {
	switch s := s.(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		return b.findBlock(cfg.KindUnreachable, s)
	case *ast.ExprStmt:
		if call, ok := s.X.(*ast.CallExpr); ok {
			if _, never := b.terms[call]; never {
				return b.findBlock(cfg.KindUnreachable, s)
			}
		}
		return b.nodeBlock[s]
	case *ast.IfStmt:
		return b.findBlock(cfg.KindIfDone, s)
	case *ast.ForStmt:
		return b.findBlock(cfg.KindForDone, s)
	case *ast.RangeStmt:
		return b.findBlock(cfg.KindRangeDone, s)
	case *ast.SwitchStmt, *ast.TypeSwitchStmt:
		return b.findBlock(cfg.KindSwitchDone, s)
	case *ast.SelectStmt:
		return b.findBlock(cfg.KindSelectDone, s)
	case *ast.BlockStmt:
		if len(s.List) > 0 {
			return b.blockAfter(s.List[len(s.List)-1])
		}
		return b.blockBefore(s)
	case *ast.LabeledStmt:
		return b.blockAfter(s.Stmt)
	case *ast.DeclStmt:
		if d, ok := s.Decl.(*ast.GenDecl); ok && d.Tok == token.VAR && len(d.Specs) > 0 {
			return b.nodeBlock[d.Specs[len(d.Specs)-1]]
		}
		return b.blockBefore(s)
	}
	if rb := b.nodeBlock[s]; rb != nil {
		return rb
	}
	return b.blockBefore(s)
}

// labelJumps names the edges created by break, continue, goto and
// fallthrough, which go/cfg represents as plain jumps.
func (b *builder) labelJumps() {
	ast.Inspect(b.body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		bs, ok := n.(*ast.BranchStmt)
		if !ok {
			return true
		}
		rb := b.blockBefore(bs)
		if rb == nil || len(rb.Succs) != 1 {
			return true
		}
		label := bs.Tok.String()
		if bs.Label != nil {
			label += " " + bs.Label.Name
		}
		b.jumpLabel[rb.Index] = label
		return true
	})
}

func (b *builder) text(n ast.Node) string { return oneLine(b.p.NodeText(n), 100) }

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

func (b *builder) line(pos token.Pos) int {
	_, l, _ := b.p.Pos(pos)
	return l
}

func (b *builder) out(rb *cfg.Block) *Block {
	if blk, ok := b.blocks[rb.Index]; ok {
		return blk
	}
	blk := &Block{raw: rb, ID: -1}
	b.blocks[rb.Index] = blk
	return blk
}

func (b *builder) edge(from *cfg.Block, to *cfg.Block, label, kind string) *Edge {
	src := b.out(from)
	dst := b.out(to)
	for _, e := range src.succs {
		if e.to == dst {
			if label != "" && !strings.Contains(e.Label, label) {
				e.Label = joinLabel(e.Label, label)
			}
			return e
		}
	}
	e := &Edge{Label: label, Kind: kind, to: dst}
	src.succs = append(src.succs, e)
	return e
}

func joinLabel(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + ", " + b
}

// convert turns go/cfg blocks into decisions and edges, collapsing switch
// and select chains into multi-way decisions.
func (b *builder) convert() {
	for _, rb := range b.raw.Blocks {
		if b.absorbed[rb.Index] {
			continue
		}
		blk := b.out(rb)
		switch len(rb.Succs) {
		case 1:
			b.edge(rb, rb.Succs[0], b.jumpLabel[rb.Index], EdgeJump)
		case 2:
			s0, s1 := rb.Succs[0], rb.Succs[1]
			switch s0.Kind {
			case cfg.KindIfThen:
				b.cond(blk, rb, DecIf)
				b.edge(rb, s0, "true", EdgeBranch)
				b.edge(rb, s1, "false", EdgeBranch)
			case cfg.KindForBody:
				b.cond(blk, rb, DecFor)
				b.edge(rb, s0, "true", EdgeBranch)
				b.edge(rb, s1, "false", EdgeBranch)
			case cfg.KindRangeBody:
				rs := s0.Stmt.(*ast.RangeStmt)
				blk.Decision = DecRange
				head := "range " + b.text(rs.X)
				if rs.Key != nil {
					kv := b.text(rs.Key)
					if rs.Value != nil {
						kv += ", " + b.text(rs.Value)
					}
					head = kv + " " + rs.Tok.String() + " " + head
				}
				blk.Cond, blk.CondLine = head, b.line(rs.For)
				for _, n := range []ast.Node{rs.X, rs.Key, rs.Value} {
					if n != nil && !isNilNode(n) {
						b.removed[n] = true
					}
				}
				b.edge(rb, s0, "next", EdgeBranch)
				b.edge(rb, s1, "done", EdgeBranch)
			case cfg.KindSwitchCaseBody:
				b.switchChain(blk, rb)
			case cfg.KindSelectCaseBody:
				b.selectChain(blk, rb)
			default:
				b.edge(rb, s0, "true", EdgeBranch)
				b.edge(rb, s1, "false", EdgeBranch)
			}
		}
	}
}

func isNilNode(n ast.Node) bool {
	switch v := n.(type) {
	case ast.Expr:
		return v == nil
	}
	return n == nil
}

// cond moves the last node of rb (an if or for condition) into the block's
// decision.
func (b *builder) cond(blk *Block, rb *cfg.Block, kind string) {
	blk.Decision = kind
	if len(rb.Nodes) == 0 {
		return
	}
	last := rb.Nodes[len(rb.Nodes)-1]
	if e, ok := last.(ast.Expr); ok {
		blk.Cond, blk.CondLine = b.text(e), b.line(e.Pos())
		b.removed[last] = true
		blk.ErrorCheck = b.isErrorCheck(e)
	}
}

func (b *builder) switchChain(blk *Block, rb *cfg.Block) {
	cc := rb.Succs[0].Stmt.(*ast.CaseClause)
	sw := b.switchOf[cc]
	hasDefault := false
	switch s := sw.(type) {
	case *ast.SwitchStmt:
		blk.Decision = DecSwitch
		if s.Tag != nil {
			blk.Cond = b.text(s.Tag)
			b.removed[s.Tag] = true
		} else {
			blk.Cond = "true"
		}
		blk.CondLine = b.line(s.Switch)
		for _, c := range s.Body.List {
			hasDefault = hasDefault || c.(*ast.CaseClause).List == nil
		}
	case *ast.TypeSwitchStmt:
		blk.Decision = DecTypeSwitch
		blk.Cond = b.text(s.Assign)
		blk.CondLine = b.line(s.Switch)
		b.removed[s.Assign] = true
		for _, c := range s.Body.List {
			hasDefault = hasDefault || c.(*ast.CaseClause).List == nil
		}
	}
	cur := rb
	for {
		body, next := cur.Succs[0], cur.Succs[1]
		clause := body.Stmt.(*ast.CaseClause)
		var exprs []string
		for _, e := range clause.List {
			exprs = append(exprs, b.text(e))
			b.removed[e] = true
		}
		b.edge(rb, body, "case "+strings.Join(exprs, ", "), EdgeBranch)
		if cur != rb {
			b.absorbed[cur.Index] = true
		}
		if next.Kind == cfg.KindSwitchNextCase && len(next.Succs) == 2 && next.Succs[0].Kind == cfg.KindSwitchCaseBody &&
			b.switchOf[next.Succs[0].Stmt.(*ast.CaseClause)] == sw {
			cur = next
			continue
		}
		// End of the chain: the default clause or the statement after the switch.
		label := "no case matched"
		if hasDefault {
			label = "default"
		}
		if len(next.Nodes) == 0 && len(next.Succs) == 1 {
			b.absorbed[next.Index] = true
			b.edge(rb, next.Succs[0], label, EdgeBranch)
		} else {
			b.edge(rb, next, label, EdgeBranch)
		}
		return
	}
}

func (b *builder) selectChain(blk *Block, rb *cfg.Block) {
	clause := rb.Succs[0].Stmt.(*ast.CommClause)
	sel := b.selectOf[clause]
	blk.Decision, blk.Cond, blk.CondLine = DecSelect, "select", b.line(sel.Select)
	hasDefault := false
	for _, c := range sel.Body.List {
		cc := c.(*ast.CommClause)
		if cc.Comm == nil {
			hasDefault = true
		} else {
			b.removed[cc.Comm] = true
		}
	}
	cur := rb
	for {
		body, next := cur.Succs[0], cur.Succs[1]
		cc := body.Stmt.(*ast.CommClause)
		if as, ok := cc.Comm.(*ast.AssignStmt); ok && len(as.Lhs) > 0 {
			b.removed[as.Lhs[0]] = true
		}
		b.edge(rb, body, "case "+b.text(cc.Comm), EdgeBranch)
		if cur != rb {
			b.absorbed[cur.Index] = true
		}
		if next.Kind == cfg.KindSelectAfterCase && len(next.Succs) == 2 && next.Succs[0].Kind == cfg.KindSelectCaseBody &&
			b.selectOf[next.Succs[0].Stmt.(*ast.CommClause)] == sel {
			cur = next
			continue
		}
		switch {
		case hasDefault && len(next.Nodes) == 0 && len(next.Succs) == 1:
			b.absorbed[next.Index] = true
			b.edge(rb, next.Succs[0], "default", EdgeBranch)
		case hasDefault:
			b.edge(rb, next, "default", EdgeBranch)
		default:
			// Without default a select waits for a case: go/cfg's fall-through
			// to the end of the chain is not a real outcome.
			b.absorbed[next.Index] = true
		}
		return
	}
}

// isErrorCheck reports whether a condition tests an error value: err != nil,
// err == nil, errors.Is or errors.As.
func (b *builder) isErrorCheck(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			if x.Op == token.NEQ || x.Op == token.EQL {
				if (isNilIdent(b.info, x.Y) && isError(b.info.TypeOf(x.X))) || (isNilIdent(b.info, x.X) && isError(b.info.TypeOf(x.Y))) {
					found = true
				}
			}
		case *ast.CallExpr:
			if f, ok := typeutil.Callee(b.info, x).(*types.Func); ok {
				if n := f.FullName(); n == "errors.Is" || n == "errors.As" {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

var errorType = types.Universe.Lookup("error").Type()

func isError(t types.Type) bool {
	if t == nil {
		return false
	}
	if types.Identical(t, errorType) {
		return true
	}
	_, isIface := t.Underlying().(*types.Interface)
	return isIface && types.Implements(t, errorType.Underlying().(*types.Interface))
}

func isNilIdent(info *types.Info, e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	_, isNil := info.Uses[id].(*types.Nil)
	return isNil
}

// stmtsOf converts the remaining nodes of a block.
func (b *builder) stmtsOf(blk *Block) {
	results := b.fn.Signature.Results()
	lastIsError := results.Len() > 0 && isError(results.At(results.Len()-1).Type())
	for _, n := range blk.raw.Nodes {
		if b.removed[n] {
			continue
		}
		st := Stmt{Line: b.line(n.Pos()), Text: b.text(n), Kind: "stmt"}
		switch x := n.(type) {
		case *ast.ReturnStmt:
			st.Kind = "return"
			if b.p.Text(x.Return, x.Return+1) == "}" {
				st.Text, st.Kind = "(end of function)", "implicit return"
			} else if lastIsError && len(x.Results) == results.Len() && !isNilIdent(b.info, x.Results[len(x.Results)-1]) {
				blk.ReturnsError = true
			}
		case *ast.DeferStmt:
			st.Kind = "defer"
			blk.Defers++
		case *ast.GoStmt:
			st.Kind = "go"
		case *ast.SendStmt:
			st.Kind = "send"
		case *ast.AssignStmt, *ast.IncDecStmt:
			st.Kind = "assign"
		case *ast.ValueSpec:
			st.Kind, st.Text = "var", "var "+st.Text
		case *ast.ExprStmt:
			if call, ok := x.X.(*ast.CallExpr); ok {
				if t, ok := b.terms[call]; ok {
					st.Kind = t
				}
			}
		case ast.Expr:
			st.Kind = "expr"
		}
		blk.Stmts = append(blk.Stmts, st)
		blk.nodes = append(blk.nodes, n)
	}
}

// finish simplifies the graph, numbers blocks and computes loops, exits,
// paths and metrics.
func (b *builder) finish(g *Graph, opt Options) {
	var all []*Block
	for _, rb := range b.raw.Blocks {
		if b.absorbed[rb.Index] {
			continue
		}
		blk := b.out(rb)
		b.stmtsOf(blk)
		all = append(all, blk)
	}
	entry := b.out(b.raw.Blocks[0])
	entry.Entry = true

	// Splice out empty pass-through blocks (merge points, labels, jumps).
	for changed := true; changed; {
		changed = false
		for _, blk := range all {
			if blk.Entry || len(blk.Stmts) > 0 || blk.Decision != "" || len(blk.succs) != 1 || blk.succs[0].to == blk || isLoopKind(blk.raw) {
				continue
			}
			next := blk.succs[0]
			for _, other := range all {
				for _, e := range other.succs {
					if e.to == blk {
						e.to = next.to
						e.Label = joinLabel(e.Label, next.Label)
						if e.Kind == EdgeJump && next.Kind != EdgeJump {
							e.Kind = next.Kind
						}
						changed = true
					}
				}
			}
			if changed {
				blk.succs = nil
				blk.raw = nil
				break
			}
		}
		if changed {
			var keep []*Block
			for _, blk := range all {
				if blk.raw != nil {
					keep = append(keep, blk)
				}
			}
			all = keep
		}
	}

	// Reachability from the entry.
	reach := map[*Block]bool{entry: true}
	order := []*Block{entry}
	for i := 0; i < len(order); i++ {
		for _, e := range order[i].succs {
			if !reach[e.to] {
				reach[e.to] = true
				order = append(order, e.to)
			}
		}
	}
	var dead []*Block
	for _, blk := range all {
		if !reach[blk] && len(blk.Stmts) > 0 {
			dead = append(dead, blk)
		}
	}
	sort.Slice(dead, func(i, j int) bool { return dead[i].raw.Index < dead[j].raw.Index })
	final := append(order, dead...)
	for i, blk := range final {
		blk.ID = i
		blk.Reachable = reach[blk]
		b.locate(blk)
	}
	g.Blocks = final
	g.Edges = []*Edge{} // a function without branches has none: [] in JSON, not null
	for _, blk := range final {
		for _, e := range blk.succs {
			e.From, e.To = blk.ID, e.to.ID
			if e.to.ID < 0 {
				continue // edge into a dropped empty dead block
			}
			g.Edges = append(g.Edges, e)
		}
	}
	g.EntryID = 0

	b.loops(g, entry)
	b.terminals(g)
	b.errorEdges(g)
	b.paths(g, opt.MaxPaths)
	b.metrics(g)
	if g.Metrics.Unreachable > 0 {
		g.Notes = append(g.Notes, fmt.Sprintf("%d block(s) cannot be reached from the entry (dead code after return, panic or an unconditional jump).", g.Metrics.Unreachable))
	}
	g.Notes = append(g.Notes, "The graph is structural: paths that appear here are allowed by the code, not observed, and some may be infeasible because their conditions contradict each other.")
}

func isLoopKind(rb *cfg.Block) bool {
	return rb != nil && (rb.Kind == cfg.KindForLoop || rb.Kind == cfg.KindRangeLoop)
}

// locate sets the block's file and line range from its statements, falling
// back to the statement that created it.
func (b *builder) locate(blk *Block) {
	file, _, _ := b.p.Pos(b.body.Pos())
	blk.File = file
	lines := []int{}
	for _, s := range blk.Stmts {
		if s.Line > 0 {
			lines = append(lines, s.Line)
		}
	}
	if blk.CondLine > 0 {
		lines = append(lines, blk.CondLine)
	}
	for _, n := range blk.nodes {
		if l := b.line(n.End()); l > 0 {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 && blk.raw != nil && blk.raw.Stmt != nil {
		lines = append(lines, b.line(blk.raw.Stmt.Pos()))
	}
	if blk.Entry && len(lines) == 0 {
		lines = append(lines, b.line(b.body.Lbrace))
	}
	sort.Ints(lines)
	if len(lines) > 0 {
		blk.Line, blk.EndLine = lines[0], lines[len(lines)-1]
	}
}

// loops marks back edges (an edge to a block on the DFS stack) and loop heads.
func (b *builder) loops(g *Graph, entry *Block) {
	state := map[*Block]int{} // 1 on stack, 2 done
	type frame struct {
		blk *Block
		i   int
	}
	stack := []frame{{entry, 0}}
	state[entry] = 1
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.i >= len(top.blk.succs) {
			state[top.blk] = 2
			stack = stack[:len(stack)-1]
			continue
		}
		e := top.blk.succs[top.i]
		top.i++
		switch state[e.to] {
		case 0:
			state[e.to] = 1
			stack = append(stack, frame{e.to, 0})
		case 1:
			e.Kind = EdgeLoop
			e.to.LoopHead = true
		}
	}
}

func (b *builder) terminals(g *Graph) {
	for _, blk := range g.Blocks {
		if len(blk.succs) > 0 {
			continue
		}
		if n := len(blk.Stmts); n > 0 {
			switch k := blk.Stmts[n-1].Kind; k {
			case "return", "implicit return", TermPanic, TermExit, TermGoexit:
				blk.Terminal = k
				continue
			}
		}
		blk.Terminal = TermBlocked
	}
}

// errorEdges marks the branch taken when an error check finds an error.
func (b *builder) errorEdges(g *Graph) {
	for _, blk := range g.Blocks {
		if !blk.ErrorCheck || blk.Decision != DecIf || len(blk.succs) != 2 {
			continue
		}
		var cond ast.Expr
		if n := len(blk.raw.Nodes); n > 0 {
			cond, _ = blk.raw.Nodes[n-1].(ast.Expr)
		}
		// Only a plain err != nil / err == nil has a known polarity; compound
		// conditions and errors.Is/As do not.
		be, ok := ast.Unparen(cond).(*ast.BinaryExpr)
		if !ok || be.Op != token.NEQ && be.Op != token.EQL {
			continue
		}
		if !(isNilIdent(b.info, be.Y) && isError(b.info.TypeOf(be.X)) || isNilIdent(b.info, be.X) && isError(b.info.TypeOf(be.Y))) {
			continue
		}
		if be.Op == token.NEQ {
			blk.succs[0].Error = true
		} else {
			blk.succs[1].Error = true
		}
	}
}

func (b *builder) paths(g *Graph, maxPaths int) {
	edgeIdx := map[*Edge]int{}
	for i, e := range g.Edges {
		edgeIdx[e] = i
	}
	// Paths go from the entry to an exit. Each loop back edge may be taken at
	// most once per path, so loop bodies appear without unbounded unrolling.
	taken := map[*Edge]bool{}
	visits := map[*Block]int{}
	var cur, curEdges []int
	var steps []string
	count, budget := 0, 200_000
	var walk func(blk *Block)
	walk = func(blk *Block) {
		if count >= MaxCount || budget <= 0 {
			return
		}
		budget--
		cur = append(cur, blk.ID)
		visits[blk]++
		defer func() { cur = cur[:len(cur)-1]; visits[blk]-- }()
		if blk.Terminal != "" {
			count++
			if len(g.Paths) < maxPaths {
				p := Path{Blocks: append([]int(nil), cur...), Edges: append([]int{}, curEdges...),
					Steps: append([]string(nil), steps...), End: blk.ID, Exit: blk.Terminal}
				p.Error = blk.ReturnsError || blk.Terminal == TermPanic || blk.Terminal == TermExit
				for _, ei := range p.Edges {
					p.Error = p.Error || g.Edges[ei].Error
				}
				g.Paths = append(g.Paths, p)
			}
			return
		}
		for _, e := range blk.succs {
			step := ""
			if e.Kind == EdgeLoop {
				if taken[e] {
					continue
				}
				taken[e] = true
				step = fmt.Sprintf("↻ B%d", e.To)
			} else if visits[e.to] >= 2 {
				continue
			} else if blk.Decision != "" && e.Label != "" {
				step = blk.Cond + " → " + e.Label
			}
			curEdges = append(curEdges, edgeIdx[e])
			if step != "" {
				steps = append(steps, step)
			}
			walk(e.to)
			curEdges = curEdges[:len(curEdges)-1]
			if step != "" {
				steps = steps[:len(steps)-1]
			}
			if e.Kind == EdgeLoop {
				taken[e] = false
			}
		}
	}
	walk(g.Blocks[0])
	g.Metrics.Paths = count
	g.Metrics.PathsSaturated = count >= MaxCount || budget <= 0
	g.Metrics.PathsListed = len(g.Paths)
	if g.Metrics.PathsSaturated {
		g.Truncated = true
		g.Status.Limit(fmt.Sprintf("path enumeration stopped after %d paths", count))
	} else if g.Metrics.PathsListed < g.Metrics.Paths {
		g.Truncated = true
		g.Status.Limit(fmt.Sprintf("%d of %d paths listed (limit %d)", g.Metrics.PathsListed, g.Metrics.Paths, maxPaths))
	}

	// Blocks that cannot reach any exit (loops without a way out).
	canExit := map[*Block]bool{}
	for changed := true; changed; {
		changed = false
		for _, blk := range g.Blocks {
			if canExit[blk] {
				continue
			}
			ok := blk.Terminal != ""
			for _, e := range blk.succs {
				ok = ok || canExit[e.to]
			}
			if ok {
				canExit[blk], changed = true, true
			}
		}
	}
	stuck := 0
	for _, blk := range g.Blocks {
		if blk.Reachable && !canExit[blk] {
			stuck++
		}
	}
	if stuck > 0 {
		g.Notes = append(g.Notes, fmt.Sprintf("%d reachable block(s) have no structural path to an exit (a loop with no break or return).", stuck))
	}
}

func (b *builder) metrics(g *Graph) {
	m := &g.Metrics
	for _, blk := range g.Blocks {
		if !blk.Reachable {
			m.Unreachable++
			continue
		}
		m.Blocks++
		if blk.Decision != "" && len(blk.succs) > 1 {
			m.DecisionPoints++
			m.Decisions += len(blk.succs) - 1
		}
		switch blk.Terminal {
		case TermReturn, TermImplicit:
			m.Returns++
		case TermPanic, TermExit, TermGoexit:
			m.Panics++
		}
		if blk.LoopHead {
			m.Loops++
		}
		m.Defers += blk.Defers
		for _, s := range blk.Stmts {
			if s.Kind == "defer" {
				g.Defers = append(g.Defers, s)
			}
		}
	}
	for _, e := range g.Edges {
		if g.Blocks[e.From].Reachable {
			m.Edges++
		}
	}
	m.Cyclomatic = m.Decisions + 1
}

// EnclosingStmt returns the innermost statement of fn containing pos, for
// navigation from a line in the editor to a block.
func EnclosingStmt(file *ast.File, pos token.Pos) ast.Stmt {
	path, _ := astutil.PathEnclosingInterval(file, pos, pos)
	for _, n := range path {
		if s, ok := n.(ast.Stmt); ok {
			return s
		}
	}
	return nil
}

// BlockAt returns the reachable block whose line range contains line.
func (g *Graph) BlockAt(line int) *Block {
	var best *Block
	for _, blk := range g.Blocks {
		if line >= blk.Line && line <= blk.EndLine {
			if best == nil || blk.EndLine-blk.Line < best.EndLine-best.Line {
				best = blk
			}
		}
	}
	return best
}
