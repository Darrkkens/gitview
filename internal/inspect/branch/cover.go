package branch

import (
	"bufio"
	"fmt"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/athos/gitview/internal/flow"
)

// Profile is a parsed `go test -coverprofile` file.
type Profile struct {
	Name    string // file name shown to the user
	Mode    string // set | count | atomic
	ModTime time.Time
	files   map[string][]coverBlock // keyed by the file name as written in the profile
}

type coverBlock struct {
	startLine, startCol, endLine, endCol int
	stmts                                int
	count                                int64
}

// CoverageSummary describes the overlay applied to a graph.
type CoverageSummary struct {
	Profile   string `json:"profile"`
	Mode      string `json:"mode"`
	Covered   int    `json:"covered"`
	Uncovered int    `json:"uncovered"`
	Partial   int    `json:"partial"`
	Unknown   int    `json:"unknown"`
	Stale     bool   `json:"stale,omitempty"`
	Note      string `json:"note"`
}

// Coverage states of a block.
const (
	Covered   = "covered"
	Uncovered = "uncovered"
	PartCover = "partial"
	Unknown   = "unknown"
)

// LoadProfile reads a coverage profile from disk.
func LoadProfile(path string) (*Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	pr, err := ParseProfile(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if st, err := f.Stat(); err == nil {
		pr.ModTime = st.ModTime()
	}
	pr.Name = filepath.Base(path)
	return pr, nil
}

// ParseProfile parses the text format written by go test -coverprofile:
//
//	mode: set
//	example.com/m/pkg/file.go:12.34,15.2 3 1
func ParseProfile(r io.Reader) (*Profile, error) {
	pr := &Profile{files: map[string][]coverBlock{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		n++
		if line == "" {
			continue
		}
		if m, ok := strings.CutPrefix(line, "mode: "); ok {
			if pr.Mode == "" {
				pr.Mode = m
			}
			continue
		}
		if pr.Mode == "" {
			return nil, fmt.Errorf("line %d: not a coverage profile (missing \"mode:\" header)", n)
		}
		colon := strings.LastIndexByte(line, ':')
		if colon < 0 {
			return nil, fmt.Errorf("line %d: malformed block %q", n, line)
		}
		file, rest := line[:colon], line[colon+1:]
		var b coverBlock
		if _, err := fmt.Sscanf(rest, "%d.%d,%d.%d %d %d", &b.startLine, &b.startCol, &b.endLine, &b.endCol, &b.stmts, &b.count); err != nil {
			return nil, fmt.Errorf("line %d: malformed block %q", n, line)
		}
		pr.files[file] = append(pr.files[file], b)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if pr.Mode == "" {
		return nil, fmt.Errorf("empty coverage profile")
	}
	return pr, nil
}

// Files lists the files named in the profile.
func (pr *Profile) Files() []string {
	out := make([]string, 0, len(pr.files))
	for f := range pr.files {
		out = append(out, f)
	}
	return out
}

func (pr *Profile) blocksFor(p *flow.Program, rel string) []coverBlock {
	var out []coverBlock
	for name, bs := range pr.files {
		if p.NormFile(name) == rel {
			out = append(out, bs...)
		}
	}
	return out
}

// Overlay marks each block of g as covered, uncovered, partially covered or
// unknown. Go counts executions of statement blocks, so coverage of a CFG
// block says its statements ran; it does not prove which edge was taken.
func (pr *Profile) Overlay(p *flow.Program, g *Graph) {
	sum := &CoverageSummary{Profile: pr.Name, Mode: pr.Mode,
		Note: "Go coverage counts statement blocks; it does not record which branch outcomes (edges) were taken."}
	g.Coverage = sum
	blocks := pr.blocksFor(p, g.Symbol.File)
	if src, err := os.Stat(filepath.Join(p.RepoRoot, g.Symbol.File)); err == nil && !pr.ModTime.IsZero() && src.ModTime().After(pr.ModTime) {
		sum.Stale = true
		sum.Note = "The source file changed after the coverage profile was written; coverage is shown as unknown. " + sum.Note
		blocks = nil
	}
	fset := p.Fset()
	for _, blk := range g.Blocks {
		var pts []token.Position
		if blk.raw != nil {
			for _, n := range blk.raw.Nodes {
				if p.Text(n.Pos(), n.Pos()+1) == "}" {
					continue // synthetic return at the closing brace
				}
				pts = append(pts, fset.Position(n.Pos()))
			}
			if len(pts) == 0 && blk.raw.Stmt != nil {
				pts = append(pts, fset.Position(blk.raw.Stmt.Pos()))
			}
		}
		state, maxCount := Unknown, int64(0)
		hit, miss := 0, 0
		for _, pt := range pts {
			found, count := false, int64(0)
			for _, cb := range blocks {
				if within(pt, cb) {
					found = true
					count = max(count, cb.count)
				}
			}
			switch {
			case !found:
			case count > 0:
				hit++
				maxCount = max(maxCount, count)
			default:
				miss++
			}
		}
		switch {
		case hit > 0 && miss == 0:
			state = Covered
		case miss > 0 && hit == 0:
			state = Uncovered
		case hit > 0 && miss > 0:
			state = PartCover
		}
		blk.Coverage, blk.CoverCount = state, maxCount
		if !blk.Reachable {
			continue
		}
		switch state {
		case Covered:
			sum.Covered++
		case Uncovered:
			sum.Uncovered++
		case PartCover:
			sum.Partial++
		default:
			sum.Unknown++
		}
	}
	if len(blocks) == 0 && !sum.Stale {
		sum.Note = "The profile has no counters for " + g.Symbol.File + "; coverage is unknown. " + sum.Note
	}
}

func within(pt token.Position, cb coverBlock) bool {
	after := pt.Line > cb.startLine || pt.Line == cb.startLine && pt.Column >= cb.startCol
	before := pt.Line < cb.endLine || pt.Line == cb.endLine && pt.Column <= cb.endCol
	return after && before
}

// CoverArgs returns the go test arguments that write a coverage profile.
func CoverArgs(out string, pkgs ...string) []string {
	return append([]string{"go", "test", "-count=1", "-covermode=set", "-coverprofile=" + out}, pkgs...)
}

func itoa(n int) string { return strconv.Itoa(n) }
