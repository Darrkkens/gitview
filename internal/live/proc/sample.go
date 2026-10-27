package proc

import (
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Sample is one reading of a process's counters. T is the offset from the
// start of the capture on the monotonic clock; Wall is informative only.
// A Has* flag is false when that counter could not be read, and its values
// must then be ignored (they are zero, not measurements).
type Sample struct {
	T    time.Duration `json:"t"`
	Wall time.Time     `json:"wall"`

	// CPU time of the whole process. CPUNanos comes from the scheduler
	// (per-thread schedstat, nanoseconds); CPUTicks from /proc/<pid>/stat
	// (USER_HZ, 10 ms resolution, includes threads that already exited).
	HasCPU   bool    `json:"hasCPU"`
	CPUNanos uint64  `json:"cpuNanos"`
	CPUTicks uint64  `json:"cpuTicks"`
	CPUPct   float64 `json:"cpuPct"` // % of one core since the previous sample; -1 on the first
	CPUExact bool    `json:"cpuExact"`

	HasMem bool   `json:"hasMem"`
	RSS    uint64 `json:"rss"`
	HWM    uint64 `json:"hwm"`
	VSZ    uint64 `json:"vsz,omitempty"` // virtual memory (VmSize)

	Threads int `json:"threads"`

	HasCtx   bool   `json:"hasCtx"`
	VolCtx   uint64 `json:"volCtx"`
	InvolCtx uint64 `json:"involCtx"`

	HasIO      bool   `json:"hasIO"`
	ReadBytes  uint64 `json:"readBytes"`  // storage reads
	WriteBytes uint64 `json:"writeBytes"` // storage writes
	RChar      uint64 `json:"rchar"`      // all read() bytes, sockets included
	WChar      uint64 `json:"wchar"`

	HasFDs bool `json:"hasFDs"`
	FDs    int  `json:"fds"`

	HasNet bool   `json:"hasNet"`
	NetRx  uint64 `json:"netRx"`
	NetTx  uint64 `json:"netTx"`
}

// Sampler reads successive samples of one process and notices when it
// exits or when its PID is reused by another process.
type Sampler struct {
	fs        FS
	pid       int
	startTick uint64
	t0        time.Time
	prev      *Sample
	tasks     map[int]uint64 // thread id → scheduler run time at the previous sample
	cpuNanos  uint64         // accumulated scheduler run time
	net       bool
}

// NewSampler starts sampling p; offsets are measured from t0, which must
// carry a monotonic reading (time.Now()).
func (f FS) NewSampler(p *Process, t0 time.Time) *Sampler {
	return &Sampler{fs: f, pid: p.PID, startTick: p.StartTicks, t0: t0, net: p.Access.Net}
}

// Sample reads the counters now. It returns ErrExited when the process is
// gone (or a zombie) and ErrRestarted when the PID belongs to another
// process.
func (s *Sampler) Sample(now time.Time) (Sample, error) {
	st, err := s.fs.readStat(s.pid)
	if errors.Is(err, ErrNoProcess) {
		return Sample{}, ErrExited
	}
	if err != nil {
		return Sample{}, err
	}
	if st.startTicks != s.startTick {
		return Sample{}, ErrRestarted
	}
	if st.state == "Z" || st.state == "X" {
		return Sample{}, ErrExited
	}
	smp := Sample{T: now.Sub(s.t0), Wall: now.Round(0), HasCPU: true, CPUTicks: st.utime + st.stime, Threads: st.threads, CPUPct: -1}

	// Scheduler run time, per thread, in nanoseconds. Threads that appear
	// count fully; the run time of a thread between the previous sample
	// and its exit is lost, so an interval in which a thread exited falls
	// back to the 10 ms tick counter.
	tasks, ok := s.readTasks()
	exact := false
	switch {
	case s.prev == nil:
		// Only differences matter; start from the tick counter.
		s.cpuNanos = st.cpuNanosFromTicks()
		exact = ok
	case ok && s.tasks != nil && !threadExited(s.tasks, tasks):
		for tid, ns := range tasks {
			if old, seen := s.tasks[tid]; !seen {
				s.cpuNanos += ns
			} else if ns >= old {
				s.cpuNanos += ns - old
			}
		}
		exact = true
	default:
		if t := smp.CPUTicks; t >= s.prev.CPUTicks {
			s.cpuNanos += (t - s.prev.CPUTicks) * uint64(time.Second/ClockTicks)
		}
	}
	s.tasks = tasks
	smp.CPUNanos = s.cpuNanos
	smp.CPUExact = exact

	if ss, err := s.fs.readStatus(s.pid); err == nil {
		smp.HasMem, smp.RSS, smp.HWM, smp.VSZ = true, ss.rss, ss.hwm, ss.vsz
		smp.HasCtx, smp.VolCtx, smp.InvolCtx = true, ss.vol, ss.inv
		if ss.threads > 0 {
			smp.Threads = ss.threads
		}
	}
	if c, err := s.fs.readIO(s.pid); err == nil {
		smp.HasIO = true
		smp.ReadBytes, smp.WriteBytes, smp.RChar, smp.WChar = c.readBytes, c.writeBytes, c.rchar, c.wchar
	}
	if n, err := s.fs.countFDs(s.pid); err == nil {
		smp.HasFDs, smp.FDs = true, n
	}
	if s.net {
		if c, err := s.fs.readNetDev(s.pid); err == nil {
			smp.HasNet, smp.NetRx, smp.NetTx = true, c[0], c[1]
		}
	}
	if s.prev != nil && smp.T > s.prev.T {
		smp.CPUPct = float64(smp.CPUNanos-s.prev.CPUNanos) / float64(smp.T-s.prev.T) * 100
	}
	p := smp
	s.prev = &p
	return smp, nil
}

func (st stat) cpuNanosFromTicks() uint64 {
	return (st.utime + st.stime) * uint64(time.Second/ClockTicks)
}

func threadExited(before, now map[int]uint64) bool {
	for tid := range before {
		if _, ok := now[tid]; !ok {
			return true
		}
	}
	return false
}

// readTasks reads the scheduler run time of every thread of the process.
func (s *Sampler) readTasks() (map[int]uint64, bool) {
	dir := s.fs.path(s.pid, "task")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}
	out := make(map[int]uint64, len(entries))
	for _, e := range entries {
		tid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile(dir + "/" + e.Name() + "/schedstat")
		if err != nil {
			continue // the thread exited between ReadDir and ReadFile
		}
		f := strings.Fields(string(data))
		if len(f) == 0 {
			return nil, false
		}
		ns, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			return nil, false
		}
		out[tid] = ns
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// Verify checks that p is still the process with its PID: ErrExited when
// it is gone (or a zombie), ErrRestarted when the PID was reused.
func (f FS) Verify(p *Process) error {
	st, err := f.readStat(p.PID)
	if errors.Is(err, ErrNoProcess) {
		return ErrExited
	}
	if err != nil {
		return err
	}
	if st.startTicks != p.StartTicks {
		return ErrRestarted
	}
	if st.state == "Z" || st.state == "X" {
		return ErrExited
	}
	return nil
}

// Supported reports whether process monitoring works on this system.
func Supported() error {
	if runtime.GOOS != "linux" {
		return errors.New("process monitoring is only implemented for Linux (/proc); this system is " + runtime.GOOS)
	}
	return nil
}
