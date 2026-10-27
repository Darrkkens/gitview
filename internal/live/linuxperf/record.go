package linuxperf

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Errors of a recording.
var (
	ErrPermission = errors.New("perf is not allowed to sample this process")
	ErrNoProcess  = errors.New("the process exited before perf attached to it")
	ErrReused     = errors.New("the PID now belongs to another process")
)

// RecordOptions configure perf record.
type RecordOptions struct {
	Bin      string
	PID      int
	Freq     int  // samples per second per thread (default 99)
	UserOnly bool // cpu-clock:u
	// TempDir is where the private directory of the recording is made
	// (default os.TempDir()).
	TempDir string
	// Verify checks that the PID still names the process chosen by the
	// user; it runs before perf starts and again once perf has attached.
	Verify func() error
}

// Event returns the perf event recorded.
func (o RecordOptions) Event() string {
	if o.UserOnly {
		return "cpu-clock:u"
	}
	return "cpu-clock"
}

// Args are the arguments of perf record (no shell is involved): call
// stacks by frame pointer (Go keeps them), CLOCK_MONOTONIC timestamps, and
// unbuffered output so that samples reach GitView as they are taken.
func (o RecordOptions) Args() []string {
	return []string{"record", "-e", o.Event(), "-F", strconv.Itoa(o.Freq), "-g", "-k", "monotonic", "--no-buffering",
		"-p", strconv.Itoa(o.PID), "-o", "-"}
}

// Recorder is a running perf record.
type Recorder struct {
	opt     RecordOptions
	cmd     *exec.Cmd
	dir     string
	file    string
	samples atomic.Int64
	lost    atomic.Int64
	bytes   atomic.Int64

	attached chan struct{}
	attOnce  sync.Once
	exited   chan struct{} // perf exited
	waitErr  error
	stderr   limitedBuffer
	stopped  atomic.Bool
	cleaned  sync.Once
}

// perf.data record types counted while recording.
const (
	recordLost        = 2
	recordSample      = 9
	recordLostSamples = 13
)

// Record starts perf record on opt.PID. It returns once perf has attached
// to the process (or failed to). Cleanup must be called eventually.
func Record(ctx context.Context, opt RecordOptions) (*Recorder, error) {
	if opt.Freq <= 0 {
		opt.Freq = 99
	}
	opt.Freq = min(opt.Freq, 999)
	if opt.PID <= 0 {
		return nil, fmt.Errorf("invalid PID %d", opt.PID)
	}
	if opt.Verify != nil {
		if err := opt.Verify(); err != nil {
			return nil, err
		}
	}
	dir, err := os.MkdirTemp(opt.TempDir, "gitview-perf-")
	if err != nil {
		return nil, err
	}
	r := &Recorder{opt: opt, dir: dir, file: filepath.Join(dir, "perf.data"), attached: make(chan struct{}), exited: make(chan struct{})}
	f, err := os.OpenFile(r.file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		r.Cleanup()
		return nil, err
	}
	r.cmd = exec.CommandContext(ctx, opt.Bin, opt.Args()...)
	r.cmd.Dir = dir
	r.cmd.Stderr = &r.stderr
	r.cmd.Env = append(os.Environ(), "LC_ALL=C")
	r.cmd.SysProcAttr = sysProcAttr()
	// SIGINT makes perf flush and exit; the context kills it outright.
	r.cmd.Cancel = func() error { return r.cmd.Process.Kill() }
	out, err := r.cmd.StdoutPipe()
	if err != nil {
		f.Close()
		r.Cleanup()
		return nil, err
	}
	if err := r.cmd.Start(); err != nil {
		f.Close()
		r.Cleanup()
		return nil, fmt.Errorf("starting perf: %w", err)
	}
	go func() {
		r.copy(out, f)
		f.Close()
		r.waitErr = r.cmd.Wait()
		close(r.exited)
	}()

	t := time.NewTimer(5 * time.Second)
	defer t.Stop()
	select {
	case <-r.attached:
	case <-r.exited:
		err := r.startError()
		r.Cleanup()
		return nil, err
	case <-t.C:
		// perf is slow to open its events (a large process): carry on, the
		// identity check below still runs.
	case <-ctx.Done():
		r.kill()
		r.Cleanup()
		return nil, ctx.Err()
	}
	if opt.Verify != nil {
		if err := opt.Verify(); err != nil {
			r.kill()
			r.Cleanup()
			return nil, err
		}
	}
	return r, nil
}

// copy writes perf's stream to f, counting its sample records. The stream
// is perf's pipe format: a 16-byte header, then records that each start
// with type (u32), misc (u16) and size (u16, header included).
func (r *Recorder) copy(out io.Reader, f *os.File) {
	w := &countWriter{w: bufio.NewWriterSize(f, 1<<16), n: &r.bytes}
	defer w.w.(*bufio.Writer).Flush()
	in := bufio.NewReaderSize(io.TeeReader(out, w), 1<<16)
	var hdr [16]byte
	if _, err := io.ReadFull(in, hdr[:]); err != nil {
		io.Copy(io.Discard, in)
		return
	}
	if string(hdr[:8]) != "PERFILE2" {
		// Another byte order or format: keep the data, count nothing.
		io.Copy(io.Discard, in)
		return
	}
	if size := binary.LittleEndian.Uint64(hdr[8:]); size > 16 {
		io.CopyN(io.Discard, in, int64(size-16))
	}
	var rec [8]byte
	for {
		if _, err := io.ReadFull(in, rec[:]); err != nil {
			return
		}
		typ := binary.LittleEndian.Uint32(rec[:4])
		size := int(binary.LittleEndian.Uint16(rec[6:8]))
		if size < 8 {
			io.Copy(io.Discard, in)
			return
		}
		body := size - 8
		switch typ {
		case recordSample:
			r.samples.Add(1)
		case recordLost, recordLostSamples:
			// The lost count is the u64 after the id.
			if body >= 16 {
				var b [16]byte
				if _, err := io.ReadFull(in, b[:]); err != nil {
					return
				}
				r.lost.Add(int64(binary.LittleEndian.Uint64(b[8:])))
				body -= 16
			}
		}
		r.attOnce.Do(func() { close(r.attached) })
		if _, err := io.CopyN(io.Discard, in, int64(body)); err != nil {
			return
		}
	}
}

type countWriter struct {
	w io.Writer
	n *atomic.Int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n.Add(int64(n))
	return n, err
}

// Samples is the number of samples written so far (exact: counted in the
// stream perf writes).
func (r *Recorder) Samples() int64 { return r.samples.Load() }

// Exited is closed when perf exits: after Stop, or by itself when the
// process it samples exits.
func (r *Recorder) Exited() <-chan struct{} { return r.exited }

// Stderr is the end of what perf printed.
func (r *Recorder) Stderr() string { return r.stderr.String() }

func (r *Recorder) kill() {
	if r.cmd.Process != nil {
		r.cmd.Process.Kill()
	}
	<-r.exited
}

// startError explains why perf exited before attaching.
func (r *Recorder) startError() error {
	msg := strings.TrimSpace(r.stderr.String())
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "perf_event_paranoid") || strings.Contains(low, "permission") ||
		strings.Contains(low, "not permitted") || strings.Contains(low, "access to performance monitoring"):
		return fmt.Errorf("%w: %s", ErrPermission, summarize(msg))
	case strings.Contains(low, "no such process") || strings.Contains(low, "couldn't find thread") || strings.Contains(low, "esrch"):
		return ErrNoProcess
	}
	if msg == "" && r.waitErr != nil {
		msg = r.waitErr.Error()
	}
	return fmt.Errorf("perf record failed: %s", summarize(msg))
}

// summarize keeps the first meaningful lines of perf's message.
func summarize(msg string) string {
	var keep []string
	for _, l := range strings.Split(msg, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "[ perf record:") {
			continue
		}
		keep = append(keep, l)
		if len(keep) == 3 {
			break
		}
	}
	if len(keep) == 0 {
		return "no message"
	}
	return strings.Join(keep, " ")
}

// RecordResult describes a finished recording.
type RecordResult struct {
	File    string // perf.data (pipe format) inside the private directory
	Samples int64
	Lost    int64
	Bytes   int64
	// Early: perf exited before Stop (the process exited).
	Early bool
}

// Stop ends the recording: SIGINT (perf flushes its buffers), then a kill
// after the grace period.
func (r *Recorder) Stop(grace time.Duration) (RecordResult, error) {
	early := false
	select {
	case <-r.exited:
		early = !r.stopped.Load()
	default:
	}
	r.stopped.Store(true)
	if !early {
		r.cmd.Process.Signal(syscall.SIGINT)
		t := time.NewTimer(grace)
		select {
		case <-r.exited:
		case <-t.C:
			r.cmd.Process.Kill()
			<-r.exited
		}
		t.Stop()
	}
	res := RecordResult{File: r.file, Samples: r.samples.Load(), Lost: r.lost.Load(), Bytes: r.bytes.Load(), Early: early}
	if res.Samples == 0 && res.Bytes <= 16 {
		if err := r.startError(); early && err != nil {
			return res, err
		}
	}
	return res, nil
}

// Cleanup removes the private directory (perf.data included).
func (r *Recorder) Cleanup() {
	r.cleaned.Do(func() { os.RemoveAll(r.dir) })
}

// limitedBuffer keeps the first 16 KiB written to it.
type limitedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := 16<<10 - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
