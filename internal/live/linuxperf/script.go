package linuxperf

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RawFrame is one line of a perf script call chain.
type RawFrame struct {
	IP      uint64
	Sym     string // "[unknown]" when perf could not name it
	Off     uint64 // offset of IP in Sym (symoff)
	DSO     string
	Inlined bool
}

// RawSample is one sample of perf script.
type RawSample struct {
	TID    int
	Time   int64 // CLOCK_MONOTONIC, ns
	Period int64 // ns for cpu-clock
	Frames []RawFrame
}

// Unknown is perf's name for an unresolved symbol or object.
const Unknown = "[unknown]"

// ScriptArgs are the arguments of perf script for a recording.
func ScriptArgs(file string, inline bool) []string {
	a := []string{"script", "-i", file, "-F", "tid,time,period,ip,sym,symoff,dso", "--ns"}
	if !inline {
		a = append(a, "--no-inline")
	}
	return a
}

// Script runs perf script on a recording and parses its output, sample by
// sample. Resolving inlined frames can be slow on large binaries: after
// inlineBudget the run is restarted without them.
func Script(ctx context.Context, bin, file string, inlineBudget time.Duration, each func(RawSample)) (inlined bool, err error) {
	if inlineBudget > 0 {
		ictx, cancel := context.WithTimeout(ctx, inlineBudget)
		var got []RawSample
		err := runScript(ictx, bin, ScriptArgs(file, true), func(s RawSample) { got = append(got, s) })
		cancel()
		if err == nil {
			for _, s := range got {
				each(s)
			}
			return true, nil
		}
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
	}
	return false, runScript(ctx, bin, ScriptArgs(file, false), each)
}

func runScript(ctx context.Context, bin string, args []string, each func(RawSample)) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.SysProcAttr = sysProcAttr()
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting perf script: %w", err)
	}
	perr := Parse(out, each)
	if perr != nil {
		io.Copy(io.Discard, out)
	}
	werr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if werr != nil {
		return fmt.Errorf("perf script: %s", summarize(stderr.String()+"\n"+werr.Error()))
	}
	return perr
}

var symOff = regexp.MustCompile(`\+0x([0-9a-f]+)$`)

// Parse reads perf script output (-F tid,time,period,ip,sym,symoff,dso):
// a header line per sample, then one tab-indented line per frame, leaf
// first, and a blank line.
func Parse(r io.Reader, each func(RawSample)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	var cur *RawSample
	flush := func() {
		if cur != nil {
			each(*cur)
			cur = nil
		}
	}
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		// Frames start with a tab; a sample's header may start with spaces
		// (perf right-aligns the thread id).
		if line[0] != '\t' {
			flush()
			s, ok := parseHeader(line)
			if !ok {
				continue // a message of perf script, not a sample
			}
			cur = &s
			continue
		}
		if cur == nil {
			continue
		}
		if f, ok := parseFrame(line); ok {
			cur.Frames = append(cur.Frames, f)
		}
	}
	flush()
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// parseHeader reads "tid time: period" (perf may add the event name).
func parseHeader(line string) (RawSample, bool) {
	f := strings.Fields(line)
	if len(f) < 3 {
		return RawSample{}, false
	}
	tid, err := strconv.Atoi(f[0])
	if err != nil {
		return RawSample{}, false
	}
	t, ok := parseTime(strings.TrimSuffix(f[1], ":"))
	if !ok {
		return RawSample{}, false
	}
	period, err := strconv.ParseInt(f[2], 10, 64)
	if err != nil {
		return RawSample{}, false
	}
	return RawSample{TID: tid, Time: t, Period: period}, true
}

// parseTime reads "seconds.nanoseconds" exactly (no float rounding).
func parseTime(s string) (int64, bool) {
	sec, frac, _ := strings.Cut(s, ".")
	n, err := strconv.ParseInt(sec, 10, 64)
	if err != nil {
		return 0, false
	}
	if len(frac) > 9 {
		frac = frac[:9]
	}
	frac += strings.Repeat("0", 9-len(frac))
	ns, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, false
	}
	return n*1e9 + ns, true
}

// parseFrame reads "ip sym+0xoff (dso)" or "ip sym (inlined)".
func parseFrame(line string) (RawFrame, bool) {
	line = strings.TrimSpace(line)
	ipStr, rest, ok := strings.Cut(line, " ")
	if !ok {
		return RawFrame{}, false
	}
	ip, err := strconv.ParseUint(ipStr, 16, 64)
	if err != nil {
		return RawFrame{}, false
	}
	f := RawFrame{IP: ip, Sym: Unknown, DSO: Unknown}
	rest = strings.TrimSpace(rest)
	if i := strings.LastIndex(rest, " ("); i >= 0 && strings.HasSuffix(rest, ")") {
		dso := rest[i+2 : len(rest)-1]
		rest = strings.TrimSpace(rest[:i])
		if dso == "inlined" {
			f.Inlined = true
			f.DSO = ""
		} else {
			f.DSO = dso
		}
	}
	if m := symOff.FindStringSubmatchIndex(rest); m != nil {
		f.Off, _ = strconv.ParseUint(rest[m[2]:m[3]], 16, 64)
		rest = rest[:m[0]]
	}
	if rest != "" {
		f.Sym = rest
	}
	return f, true
}
