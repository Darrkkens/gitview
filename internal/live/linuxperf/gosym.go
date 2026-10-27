package linuxperf

import (
	"bufio"
	"bytes"
	"debug/elf"
	"debug/gosym"
	"errors"
	"io"
	"strconv"
	"strings"
)

// goTable resolves addresses and functions of a Go executable from its
// pclntab, which Go keeps even in binaries stripped with -s (perf, reading
// the ELF symbol table, then sees only [unknown]).
type goTable struct {
	t    *gosym.Table
	bias uint64 // load address of a position-independent executable
	text [2]uint64
}

// openGoTable reads the pclntab of an executable. base is where a
// position-independent executable was loaded (from /proc/<pid>/maps).
func openGoTable(r io.ReaderAt, base uint64) (*goTable, error) {
	f, err := elf.NewFile(r)
	if err != nil {
		return nil, err
	}
	pcln := f.Section(".gopclntab")
	text := f.Section(".text")
	if pcln == nil || text == nil {
		return nil, errors.New("no Go symbol table (.gopclntab) in the executable")
	}
	data, err := pcln.Data()
	if err != nil {
		return nil, err
	}
	t, err := gosym.NewTable(nil, gosym.NewLineTable(data, text.Addr))
	if err != nil {
		return nil, err
	}
	g := &goTable{t: t, text: [2]uint64{text.Addr, text.Addr + text.Size}}
	if f.Type == elf.ET_DYN {
		g.bias = base
	}
	return g, nil
}

// loadBase is the start of the first mapping of exe (file offset 0) in the
// content of /proc/<pid>/maps.
func loadBase(maps []byte, exe string) uint64 {
	sc := bufio.NewScanner(bytes.NewReader(maps))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 6 || strings.Trim(f[2], "0") != "" {
			continue
		}
		if path := strings.TrimSuffix(strings.Join(f[5:], " "), " (deleted)"); path != exe {
			continue
		}
		lo, _, _ := strings.Cut(f[0], "-")
		if n, err := strconv.ParseUint(lo, 16, 64); err == nil {
			return n
		}
	}
	return 0
}

// pos is a source position.
type pos struct {
	file      string
	line      int
	startLine int
}

// atOffset resolves the position of name+off: the line of the innermost
// inlined call at that address, and the function's own first line.
func (g *goTable) atOffset(name string, off uint64) (pos, bool) {
	fn := g.t.LookupFunc(name)
	if fn == nil {
		return pos{}, false
	}
	file, line, _ := g.t.PCToLine(fn.Entry + off)
	_, start, _ := g.t.PCToLine(fn.Entry)
	return pos{file: file, line: line, startLine: start}, file != ""
}

// entry resolves the file and first line of a function, by name.
func (g *goTable) entry(name string) (pos, bool) {
	fn := g.t.LookupFunc(name)
	if fn == nil {
		return pos{}, false
	}
	file, line, _ := g.t.PCToLine(fn.Entry)
	return pos{file: file, line: line, startLine: line}, file != ""
}

// byAddress names an address perf could not resolve.
func (g *goTable) byAddress(ip uint64) (string, pos, bool) {
	pc := ip - g.bias
	if pc < g.text[0] || pc >= g.text[1] {
		return "", pos{}, false
	}
	file, line, fn := g.t.PCToLine(pc)
	if fn == nil {
		return "", pos{}, false
	}
	_, start, _ := g.t.PCToLine(fn.Entry)
	return fn.Name, pos{file: file, line: line, startLine: start}, true
}
