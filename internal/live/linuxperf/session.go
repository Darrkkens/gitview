package linuxperf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// Options configure perf in a capture.
type Options struct {
	Cap  Capability
	PID  int
	Freq int // default 99
	// Verify checks the identity of the process (PID reuse).
	Verify func() error
	// T0 is CLOCK_MONOTONIC at the start of the capture (Monotonic()).
	T0 int64
	// Dir is the capture's directory, where DataFile is written.
	Dir string
	// Proc is the procfs root (default /proc).
	Proc string
	// TempDir is where perf's private directory is made (default os.TempDir).
	TempDir string
}

// Session is perf sampling a process during a capture.
type Session struct {
	opt   Options
	rec   *Recorder
	start time.Duration
	exe   string   // the executable as /proc/<pid>/maps names it
	bin   *os.File // /proc/<pid>/exe, opened while the process runs
	maps  []byte
}

// Start attaches perf to the process; start is the offset of this moment
// in the capture.
func Start(ctx context.Context, opt Options, start time.Duration) (*Session, error) {
	if !opt.Cap.Available {
		return nil, errors.New(opt.Cap.Reason)
	}
	if opt.Proc == "" {
		opt.Proc = "/proc"
	}
	if opt.Freq <= 0 {
		opt.Freq = 99
	}
	s := &Session{opt: opt, start: start}
	rec, err := Record(ctx, RecordOptions{Bin: opt.Cap.Bin, PID: opt.PID, Freq: opt.Freq, UserOnly: opt.Cap.UserOnly, TempDir: opt.TempDir, Verify: opt.Verify})
	if err != nil {
		return nil, err
	}
	s.rec = rec
	// The executable is kept open: its Go symbol table stays readable when
	// the process exits or the file is replaced by a new build.
	pdir := opt.Proc + "/" + strconv.Itoa(opt.PID)
	if link, err := os.Readlink(pdir + "/exe"); err == nil {
		s.exe = strings.TrimSuffix(link, " (deleted)")
	}
	if f, err := os.Open(pdir + "/exe"); err == nil {
		s.bin = f
	}
	s.maps, _ = os.ReadFile(pdir + "/maps")
	if opt.Verify != nil {
		// The files above must belong to the process perf attached to.
		if err := opt.Verify(); err != nil {
			s.Abort()
			return nil, err
		}
	}
	return s, nil
}

// Samples is the number of samples recorded so far (exact).
func (s *Session) Samples() int64 { return s.rec.Samples() }

// Exited is closed when perf exits by itself (the process exited).
func (s *Session) Exited() <-chan struct{} { return s.rec.Exited() }

// Abort stops perf and removes everything, keeping nothing.
func (s *Session) Abort() {
	s.rec.kill()
	s.rec.Cleanup()
	if s.bin != nil {
		s.bin.Close()
	}
}

// Stop ends the recording at offset end, processes the samples with perf
// script and writes DataFile into the capture's directory. The temporary
// perf.data is always removed. Errors are reported in the result: perf
// failing never fails the capture.
func (s *Session) Stop(ctx context.Context, end time.Duration) Result {
	defer s.rec.Cleanup()
	if s.bin != nil {
		defer s.bin.Close()
	}
	c := s.opt.Cap
	res := Result{Bin: c.Bin, Version: c.Version, Event: RecordOptions{UserOnly: c.UserOnly}.Event(), Freq: s.opt.Freq, UserOnly: c.UserOnly, Start: s.start, End: end}
	rr, err := s.rec.Stop(10 * time.Second)
	res.Samples, res.Lost, res.Early = rr.Samples, rr.Lost, rr.Early
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if rr.Samples == 0 {
		res.Notes = append(res.Notes, "perf recorded no sample: the process did not run on a CPU while it was sampled (idle), or it exited at once.")
		return res
	}
	var bin io.ReaderAt // (an untyped nil when the file is missing)
	if s.bin != nil {
		bin = s.bin
	}
	b, err := NewBuilder(s.opt.T0, s.exe, bin, s.maps)
	if bin == nil {
		err = errors.New("the executable could not be opened")
	}
	if err != nil {
		res.Notes = append(res.Notes, "Go symbol table not read ("+err.Error()+"): frames are named by perf only, without source lines.")
	}
	sctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	res.Inlined, err = Script(sctx, c.Bin, rr.File, time.Minute, b.Add)
	if err != nil {
		res.Error = "processing the samples (perf script): " + err.Error()
		return res
	}
	if !res.Inlined {
		res.Notes = append(res.Notes, "Inlined calls are not shown: resolving them took too long (perf script --no-inline).")
	}
	d := b.Data()
	if len(d.T) == 0 {
		res.Error = fmt.Sprintf("perf recorded %d samples but perf script returned none of them", rr.Samples)
		return res
	}
	if _, err := d.Save(s.opt.Dir); err != nil {
		res.Error = "saving the samples: " + err.Error()
		return res
	}
	res.File = DataFile
	return res
}
