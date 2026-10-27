package linuxperf

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DataFile is the name of the processed samples in a capture directory.
const DataFile = "perf.json.gz"

// Frame is one function of a call stack.
type Frame struct {
	Func  string `json:"fn"`
	File  string `json:"file,omitempty"`
	Line  int    `json:"line,omitempty"`
	Start int    `json:"start,omitempty"` // first line of the function
	DSO   string `json:"dso,omitempty"`
	// By says who named the frame: "perf" (ELF symbols), "pclntab" (Go's
	// own table, read by GitView) or "" (unresolved).
	By string `json:"by,omitempty"`
}

// Data are the processed samples of a recording. Times are offsets from
// the start of the capture on CLOCK_MONOTONIC.
type Data struct {
	Frames []Frame   `json:"frames"`
	Stacks [][]int32 `json:"stacks"` // frame indexes, leaf first
	T      []int64   `json:"t"`      // per sample: ns since the capture started
	S      []int32   `json:"s"`      // per sample: stack index
	P      []int64   `json:"p"`      // per sample: period (ns of CPU for cpu-clock)
	Stats  Stats     `json:"stats"`
}

// Stats describe the quality of the samples.
type Stats struct {
	Samples     int `json:"samples"`
	UnknownLeaf int `json:"unknownLeaf"` // samples whose own function could not be named
	Broken      int `json:"broken"`      // samples whose call chain ends in an unresolved frame
	ByPclntab   int `json:"byPclntab"`   // frames GitView named from Go's pclntab
	Kernel      int `json:"kernel"`      // samples taken in the kernel
	Foreign     int `json:"foreign"`     // samples outside the executable (C libraries, vdso)
}

// Builder turns perf script samples into Data.
type Builder struct {
	d      Data
	frames map[Frame]int32
	stacks map[string]int32
	g      *goTable
	exe    string // the executable as perf names it
	t0     int64  // CLOCK_MONOTONIC at the start of the capture
}

// NewBuilder prepares the processing of samples of a process. exe is the
// executable's path as perf and /proc/<pid>/maps name it; bin, when not
// nil, is the executable itself (its pclntab names Go frames) and maps the
// content of /proc/<pid>/maps. The error only concerns the Go table: the
// builder is usable without it.
func NewBuilder(t0 int64, exe string, bin io.ReaderAt, maps []byte) (*Builder, error) {
	b := &Builder{frames: map[Frame]int32{}, stacks: map[string]int32{}, exe: exe, t0: t0}
	var err error
	if bin != nil {
		b.g, err = openGoTable(bin, loadBase(maps, exe))
	}
	return b, err
}

func (b *Builder) frame(f Frame) int32 {
	if i, ok := b.frames[f]; ok {
		return i
	}
	i := int32(len(b.d.Frames))
	b.d.Frames = append(b.d.Frames, f)
	b.frames[f] = i
	return i
}

// cleanSym drops the ABI suffix of assembly functions (pprof shows them
// without it).
func cleanSym(s string) string {
	for _, suf := range []string{".abi0", ".abiinternal"} {
		s = strings.TrimSuffix(s, suf)
	}
	return s
}

func (b *Builder) inExe(dso string) bool {
	return b.exe != "" && (dso == b.exe || strings.TrimSuffix(dso, " (deleted)") == b.exe)
}

// Add processes one sample.
func (b *Builder) Add(s RawSample) {
	if len(s.Frames) == 0 {
		return
	}
	var stack []int32
	var key strings.Builder
	broken := false
	leafIP := s.Frames[0].IP
	for i := 0; i < len(s.Frames); i++ {
		rf := s.Frames[i]
		// A caller's address is the return address: the instruction after
		// the call. Its position is looked up one byte earlier, inside the
		// call, as pprof does; perf resolves inlined calls at the return
		// address itself, which often lands in the next inlined call, so
		// those are left out (the call shows in the function containing it).
		caller := rf.IP != leafIP
		if caller && rf.Inlined {
			continue
		}
		var back uint64
		if caller {
			back = 1
		}
		f := Frame{Func: cleanSym(rf.Sym), DSO: rf.DSO, By: "perf"}
		if rf.Sym == Unknown {
			f.By = ""
			if b.g != nil && (rf.DSO == Unknown || b.inExe(rf.DSO)) {
				if name, p, ok := b.g.byAddress(rf.IP - back); ok {
					f = Frame{Func: name, File: p.file, Line: p.line, Start: p.startLine, DSO: b.exe, By: "pclntab"}
					b.d.Stats.ByPclntab++
				}
			}
		} else if b.g != nil && (rf.Inlined || b.inExe(rf.DSO)) {
			own, hasOwn := b.g.entry(f.Func)
			switch {
			case caller:
				// Go's line table gives the innermost inlined position: kept
				// only when it is in the function's own file.
				if p, ok := b.g.atOffset(f.Func, rf.Off-min(rf.Off, back)); ok && (!hasOwn || p.file == own.file) {
					f.File, f.Line = p.file, p.line
				}
			case i == 0 || s.Frames[i-1].IP != rf.IP:
				// The sampled instruction: perf lists the inlined calls there
				// innermost first, then the function containing them (with the
				// offset); the line table gives the innermost one's line.
				j := i
				for j < len(s.Frames) && s.Frames[j].IP == rf.IP && s.Frames[j].Inlined {
					j++
				}
				if j < len(s.Frames) && s.Frames[j].IP == rf.IP {
					if p, ok := b.g.atOffset(cleanSym(s.Frames[j].Sym), s.Frames[j].Off); ok {
						f.File, f.Line = p.file, p.line
					}
				}
			}
			if hasOwn {
				if f.File == "" {
					f.File, f.Line = own.file, own.line
				}
				f.Start = own.startLine
			}
			if rf.Inlined {
				f.DSO = b.exe
			}
		}
		if f.By == "" {
			if i > 0 {
				// An unresolved caller: the frame-pointer chain left the Go
				// code (assembly without frame pointer, C code): what follows
				// is not a call chain.
				broken = true
				break
			}
			f.Func = Unknown
		}
		idx := b.frame(f)
		stack = append(stack, idx)
		fmt.Fprintf(&key, "%d,", idx)
	}
	st := &b.d.Stats
	st.Samples++
	leaf := s.Frames[0]
	if leaf.Sym == Unknown && b.d.Frames[stack[0]].By == "" {
		st.UnknownLeaf++
	}
	if broken {
		st.Broken++
	}
	switch {
	case strings.HasPrefix(leaf.DSO, "[kernel"):
		st.Kernel++
	case !leaf.Inlined && leaf.DSO != Unknown && !b.inExe(leaf.DSO):
		st.Foreign++
	}
	si, ok := b.stacks[key.String()]
	if !ok {
		si = int32(len(b.d.Stacks))
		b.d.Stacks = append(b.d.Stacks, stack)
		b.stacks[key.String()] = si
	}
	b.d.T = append(b.d.T, s.Time-b.t0)
	b.d.S = append(b.d.S, si)
	b.d.P = append(b.d.P, s.Period)
}

// Data returns the samples in time order.
func (b *Builder) Data() *Data {
	d := &b.d
	if !sort.SliceIsSorted(d.T, func(i, j int) bool { return d.T[i] < d.T[j] }) {
		idx := make([]int, len(d.T))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(i, j int) bool { return d.T[idx[i]] < d.T[idx[j]] })
		t, s, p := make([]int64, len(idx)), make([]int32, len(idx)), make([]int64, len(idx))
		for k, i := range idx {
			t[k], s[k], p[k] = d.T[i], d.S[i], d.P[i]
		}
		d.T, d.S, d.P = t, s, p
	}
	return d
}

// Save writes the data to dir/DataFile.
func (d *Data) Save(dir string) (int64, error) {
	path := filepath.Join(dir, DataFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	zw := gzip.NewWriter(f)
	if err := json.NewEncoder(zw).Encode(d); err != nil {
		f.Close()
		return 0, err
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return 0, err
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

// Load reads dir/name.
func Load(dir, name string) (*Data, error) {
	f, err := os.Open(filepath.Join(dir, filepath.Base(name)))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	var d Data
	if err := json.NewDecoder(zr).Decode(&d); err != nil {
		return nil, err
	}
	if len(d.S) != len(d.T) || len(d.P) != len(d.T) {
		return nil, fmt.Errorf("%s: inconsistent samples", name)
	}
	for _, s := range d.S {
		if int(s) >= len(d.Stacks) || s < 0 {
			return nil, fmt.Errorf("%s: bad stack index", name)
		}
	}
	for _, st := range d.Stacks {
		for _, f := range st {
			if int(f) >= len(d.Frames) || f < 0 {
				return nil, fmt.Errorf("%s: bad frame index", name)
			}
		}
	}
	return &d, nil
}

// Between counts the samples and their CPU in [from, to).
func (d *Data) Between(from, to time.Duration) (n int, cpu int64) {
	lo := sort.Search(len(d.T), func(i int) bool { return d.T[i] >= int64(from) })
	hi := sort.Search(len(d.T), func(i int) bool { return d.T[i] >= int64(to) })
	for i := lo; i < hi; i++ {
		cpu += d.P[i]
	}
	return hi - lo, cpu
}
