// Package proc is the Process Collector of the Runtime Performance
// Inspector: it reads what Linux exposes about a running process under
// /proc — identity, Go build information, listening sockets and resource
// counters — without attaching to it and without root.
//
// Every metric says whether it could be read; a metric the kernel does not
// expose to this user is reported as unavailable with the reason, never
// estimated.
package proc

import (
	"bufio"
	"bytes"
	"debug/buildinfo"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ClockTicks is USER_HZ, the unit of the CPU times in /proc/<pid>/stat. It
// is 100 on every Linux architecture GitView supports.
const ClockTicks = 100

// FS reads a procfs tree (normally /proc; tests use a fake tree).
type FS struct {
	Root string
}

// Default is the real /proc.
var Default = FS{Root: "/proc"}

func (f FS) path(pid int, parts ...string) string {
	return filepath.Join(append([]string{f.Root, strconv.Itoa(pid)}, parts...)...)
}

// Metric names used in Access.Reasons.
const (
	MetricCPU     = "cpu"
	MetricMemory  = "memory"
	MetricThreads = "threads"
	MetricCtx     = "context switches"
	MetricIO      = "io"
	MetricFDs     = "file descriptors"
	MetricPorts   = "ports"
	MetricNet     = "network"
	MetricExe     = "executable"
)

// Access lists what can be read about a process and why the rest cannot.
type Access struct {
	Stat    bool              `json:"stat"`
	Status  bool              `json:"status"`
	IO      bool              `json:"io"`
	FD      bool              `json:"fd"`
	Exe     bool              `json:"exe"`
	Net     bool              `json:"net"`
	Reasons map[string]string `json:"reasons,omitempty"`
}

func (a *Access) deny(metric, reason string) {
	if a.Reasons == nil {
		a.Reasons = map[string]string{}
	}
	a.Reasons[metric] = reason
}

// Port is a listening socket of the process.
type Port struct {
	Proto    string `json:"proto"` // tcp | tcp6
	Addr     string `json:"addr"`
	Port     int    `json:"port"`
	Loopback bool   `json:"loopback"`
}

func (p Port) String() string {
	if strings.Contains(p.Addr, ":") {
		return fmt.Sprintf("[%s]:%d", p.Addr, p.Port)
	}
	return fmt.Sprintf("%s:%d", p.Addr, p.Port)
}

// Dial returns an address to connect to the port from this machine.
func (p Port) Dial() string {
	switch p.Addr {
	case "0.0.0.0":
		return fmt.Sprintf("127.0.0.1:%d", p.Port)
	case "::":
		return fmt.Sprintf("[::1]:%d", p.Port)
	}
	return p.String()
}

// Process describes a running process. The command line is never stored:
// it may contain secrets; only the executable and the argument count are.
type Process struct {
	PID        int       `json:"pid"`
	Comm       string    `json:"comm"`
	Exe        string    `json:"exe,omitempty"`
	ExeDeleted bool      `json:"exeDeleted,omitempty"` // binary replaced since the process started
	Args       int       `json:"args"`
	UID        int       `json:"uid"`
	StartTicks uint64    `json:"startTicks"` // identity together with PID (PIDs are reused)
	Started    time.Time `json:"started"`

	Go          bool   `json:"go"`
	GoKnown     bool   `json:"goKnown"` // false when the executable could not be read
	GoVersion   string `json:"goVersion,omitempty"`
	MainModule  string `json:"mainModule,omitempty"`
	MainVersion string `json:"mainVersion,omitempty"`
	VCSRevision string `json:"vcsRevision,omitempty"`
	VCSModified bool   `json:"vcsModified,omitempty"`

	Ports    []Port `json:"ports,omitempty"`
	OwnNetNS bool   `json:"ownNetNS,omitempty"`
	Threads  int    `json:"threads"`
	RSS      uint64 `json:"rss"`
	// CPUTime is the CPU the process used since it started (10 ms ticks);
	// CPUAvg is that time over its lifetime, in % of one core.
	CPUTime time.Duration `json:"cpuTime"`
	CPUAvg  float64       `json:"cpuAvg"`
	Access  Access        `json:"access"`
}

// Label is a short description for lists.
func (p *Process) Label() string {
	name := p.Comm
	if p.Exe != "" {
		name = filepath.Base(p.Exe)
	}
	return fmt.Sprintf("%s (pid %d)", name, p.PID)
}

var (
	ErrNoProcess = errors.New("no such process")
	ErrExited    = errors.New("the process exited")
	ErrRestarted = errors.New("the PID now belongs to another process (the original exited and the PID was reused)")
)

// stat holds the fields of /proc/<pid>/stat GitView uses.
type stat struct {
	comm       string
	state      string
	ppid       int
	utime      uint64
	stime      uint64
	threads    int
	startTicks uint64
	rssPages   uint64
}

func (f FS) readStat(pid int) (stat, error) {
	data, err := os.ReadFile(f.path(pid, "stat"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return stat{}, ErrNoProcess
		}
		return stat{}, err
	}
	return parseStat(data)
}

// parseStat parses /proc/<pid>/stat. The command name is between the first
// '(' and the last ')' and may itself contain spaces and parentheses.
func parseStat(data []byte) (stat, error) {
	s := string(data)
	open, close := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open < 0 || close < open {
		return stat{}, errors.New("malformed stat")
	}
	var st stat
	st.comm = s[open+1 : close]
	f := strings.Fields(s[close+1:])
	if len(f) < 22 {
		return stat{}, errors.New("malformed stat: too few fields")
	}
	// f[0] is field 3 (state).
	num := func(i int) uint64 { v, _ := strconv.ParseUint(f[i], 10, 64); return v }
	st.state = f[0]
	st.ppid = int(num(1))
	st.utime, st.stime = num(11), num(12)
	st.threads = int(num(17))
	st.startTicks = num(19)
	st.rssPages = num(21)
	return st, nil
}

// status holds the fields of /proc/<pid>/status GitView uses.
type status struct {
	uid      int
	rss, hwm uint64 // bytes
	vsz      uint64 // bytes of virtual memory (VmSize)
	threads  int
	vol, inv uint64
}

func (f FS) readStatus(pid int) (status, error) {
	data, err := os.ReadFile(f.path(pid, "status"))
	if err != nil {
		return status{}, err
	}
	var st status
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(v)
		if len(fields) == 0 {
			continue
		}
		n, _ := strconv.ParseUint(fields[0], 10, 64)
		switch k {
		case "Uid":
			st.uid = int(n)
		case "VmRSS":
			st.rss = n * 1024
		case "VmHWM":
			st.hwm = n * 1024
		case "VmSize":
			st.vsz = n * 1024
		case "Threads":
			st.threads = int(n)
		case "voluntary_ctxt_switches":
			st.vol = n
		case "nonvoluntary_ctxt_switches":
			st.inv = n
		}
	}
	return st, nil
}

type ioCounters struct {
	rchar, wchar, readBytes, writeBytes uint64
}

func (f FS) readIO(pid int) (ioCounters, error) {
	data, err := os.ReadFile(f.path(pid, "io"))
	if err != nil {
		return ioCounters{}, err
	}
	var c ioCounters
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		n, _ := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		switch k {
		case "rchar":
			c.rchar = n
		case "wchar":
			c.wchar = n
		case "read_bytes":
			c.readBytes = n
		case "write_bytes":
			c.writeBytes = n
		}
	}
	return c, nil
}

// bootTime returns the boot time from /proc/stat.
func (f FS) bootTime() time.Time {
	data, err := os.ReadFile(filepath.Join(f.Root, "stat"))
	if err != nil {
		return time.Time{}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			return time.Unix(n, 0)
		}
	}
	return time.Time{}
}

// permReason explains a failed read.
func permReason(err error, what string) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return what + ": permission denied (the process belongs to another user; it needs the same user or CAP_SYS_PTRACE)"
	case errors.Is(err, fs.ErrNotExist):
		return what + ": not available (kernel thread, or not exposed by this kernel)"
	}
	return what + ": " + err.Error()
}

var buildCache sync.Map // exe path + mtime + size → *buildinfo.BuildInfo or error

// Inspect reads a process: identity, Go build information, ports and the
// availability of each metric.
func (f FS) Inspect(pid int) (*Process, error) {
	return f.inspect(pid, listenCache{})
}

// listenCache holds the listening sockets of each network namespace read
// during one listing (every process of a namespace sees the same table).
type listenCache map[string]map[uint64]Port

func (f FS) inspect(pid int, lc listenCache) (*Process, error) {
	st, err := f.readStat(pid)
	if err != nil {
		return nil, err
	}
	p := &Process{PID: pid, Comm: st.comm, StartTicks: st.startTicks, Threads: st.threads}
	p.Access.Stat = true
	p.CPUTime = time.Duration(st.utime+st.stime) * time.Second / ClockTicks
	if bt := f.bootTime(); !bt.IsZero() {
		p.Started = bt.Add(time.Duration(st.startTicks) * time.Second / ClockTicks)
		if up := time.Since(p.Started); up > time.Second {
			p.CPUAvg = float64(p.CPUTime) / float64(up) * 100
		}
	}
	if ss, err := f.readStatus(pid); err == nil {
		p.Access.Status = true
		p.UID, p.RSS = ss.uid, ss.rss
	} else {
		p.Access.deny(MetricMemory, permReason(err, "status"))
	}
	if _, err := f.readIO(pid); err == nil {
		p.Access.IO = true
	} else {
		p.Access.deny(MetricIO, permReason(err, "io counters"))
	}
	if args, err := os.ReadFile(f.path(pid, "cmdline")); err == nil {
		p.Args = bytes.Count(args, []byte{0})
	}
	exeLink := f.path(pid, "exe")
	if target, err := os.Readlink(exeLink); err == nil {
		p.Access.Exe = true
		p.Exe = strings.TrimSuffix(target, " (deleted)")
		p.ExeDeleted = strings.HasSuffix(target, " (deleted)")
		p.readBuildInfo(exeLink)
	} else {
		p.Access.deny(MetricExe, permReason(err, "executable"))
	}
	inodes, err := f.socketInodes(pid)
	if err != nil {
		p.Access.deny(MetricFDs, permReason(err, "open files"))
		p.Access.deny(MetricPorts, permReason(err, "listening ports (socket ownership)"))
	} else {
		p.Access.FD = true
		p.Ports = f.listening(pid, inodes, lc)
	}
	p.OwnNetNS = f.ownNetNS(pid)
	if p.OwnNetNS {
		if _, err := f.readNetDev(pid); err == nil {
			p.Access.Net = true
		} else {
			p.Access.deny(MetricNet, permReason(err, "network namespace counters"))
		}
	} else {
		p.Access.deny(MetricNet, "Linux exposes no per-process network byte counters; the process shares this machine's network namespace, whose counters include every other process (request bytes come from the HTTP collector)")
	}
	return p, nil
}

// goBuildInfo reads the Go build information of an executable (through
// /proc/<pid>/exe, so a replaced binary still reads the running one); nil
// for executables not built by Go. Results are cached per file version.
func goBuildInfo(exeLink, exe string) *buildinfo.BuildInfo {
	key := exeLink
	if fi, err := os.Stat(exeLink); err == nil {
		key = fmt.Sprintf("%s|%d|%d", exe, fi.Size(), fi.ModTime().UnixNano())
	}
	if v, ok := buildCache.Load(key); ok {
		bi, _ := v.(*buildinfo.BuildInfo)
		return bi
	}
	bi, err := buildinfo.ReadFile(exeLink)
	if err != nil {
		bi = nil
	}
	buildCache.Store(key, bi)
	return bi
}

func (p *Process) readBuildInfo(exe string) {
	bi := goBuildInfo(exe, p.Exe)
	p.GoKnown = true
	if bi == nil {
		return
	}
	p.Go = true
	p.GoVersion = bi.GoVersion
	p.MainModule, p.MainVersion = bi.Main.Path, bi.Main.Version
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			p.VCSRevision = s.Value
		case "vcs.modified":
			p.VCSModified = s.Value == "true"
		}
	}
}

// socketInodes returns the inodes of the sockets the process holds.
func (f FS) socketInodes(pid int) (map[uint64]bool, error) {
	dir := f.path(pid, "fd")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[uint64]bool{}
	for _, e := range entries {
		target, err := os.Readlink(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if v, ok := strings.CutPrefix(target, "socket:["); ok {
			if n, err := strconv.ParseUint(strings.TrimSuffix(v, "]"), 10, 64); err == nil {
				out[n] = true
			}
		}
	}
	return out, nil
}

func (f FS) countFDs(pid int) (int, error) {
	entries, err := os.ReadDir(f.path(pid, "fd"))
	if err != nil {
		return -1, err
	}
	return len(entries), nil
}

// listening returns the TCP sockets in LISTEN state owned by the process,
// read from its own network namespace view.
func (f FS) listening(pid int, inodes map[uint64]bool, lc listenCache) []Port {
	ns, _ := os.Readlink(f.path(pid, "ns", "net"))
	table, ok := lc[ns]
	if !ok || ns == "" {
		table = f.listenTable(pid)
		if ns != "" {
			lc[ns] = table
		}
	}
	var out []Port
	for inode := range inodes {
		if p, ok := table[inode]; ok {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].Proto < out[j].Proto
	})
	return out
}

// listenTable reads the TCP sockets in LISTEN state of the process's
// network namespace, by socket inode.
func (f FS) listenTable(pid int) map[uint64]Port {
	out := map[uint64]Port{}
	for _, proto := range []string{"tcp", "tcp6"} {
		data, err := os.ReadFile(f.path(pid, "net", proto))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n")[1:] {
			fl := strings.Fields(line)
			if len(fl) < 10 || fl[3] != "0A" { // 0A = TCP_LISTEN
				continue
			}
			inode, _ := strconv.ParseUint(fl[9], 10, 64)
			addr, port, ok := parseHexAddr(fl[1])
			if inode == 0 || !ok {
				continue
			}
			out[inode] = Port{Proto: proto, Addr: addr, Port: port, Loopback: isLoopback(addr)}
		}
	}
	return out
}

// parseHexAddr decodes "0100007F:1F90" (IPv4) or a 32-digit IPv6 address,
// both stored as little-endian 32-bit words.
func parseHexAddr(s string) (string, int, bool) {
	h, p, ok := strings.Cut(s, ":")
	if !ok {
		return "", 0, false
	}
	port, err := strconv.ParseUint(p, 16, 16)
	if err != nil {
		return "", 0, false
	}
	var b []byte
	for i := 0; i+8 <= len(h); i += 8 {
		w, err := strconv.ParseUint(h[i:i+8], 16, 32)
		if err != nil {
			return "", 0, false
		}
		b = append(b, byte(w), byte(w>>8), byte(w>>16), byte(w>>24))
	}
	switch len(b) {
	case 4:
		return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3]), int(port), true
	case 16:
		// IPv4-mapped addresses (::ffff:a.b.c.d) are shown as IPv4.
		if bytes.Equal(b[:10], make([]byte, 10)) && b[10] == 0xff && b[11] == 0xff {
			return fmt.Sprintf("%d.%d.%d.%d", b[12], b[13], b[14], b[15]), int(port), true
		}
		var parts []string
		for i := 0; i < 16; i += 2 {
			parts = append(parts, strconv.FormatUint(uint64(b[i])<<8|uint64(b[i+1]), 16))
		}
		return compressIPv6(parts), int(port), true
	}
	return "", 0, false
}

func compressIPv6(parts []string) string {
	// Replace the longest run of zero groups by "::".
	best, bestLen := -1, 1
	for i := 0; i < len(parts); {
		if parts[i] != "0" {
			i++
			continue
		}
		j := i
		for j < len(parts) && parts[j] == "0" {
			j++
		}
		if j-i > bestLen {
			best, bestLen = i, j-i
		}
		i = j
	}
	if best < 0 {
		return strings.Join(parts, ":")
	}
	return strings.Join(parts[:best], ":") + "::" + strings.Join(parts[best+bestLen:], ":")
}

func isLoopback(addr string) bool {
	return strings.HasPrefix(addr, "127.") || addr == "::1"
}

// ownNetNS reports whether the process lives in a network namespace other
// than GitView's (a container), where namespace counters describe it alone.
func (f FS) ownNetNS(pid int) bool {
	theirs, err1 := os.Readlink(f.path(pid, "ns", "net"))
	ours, err2 := os.Readlink(filepath.Join(f.Root, "self", "ns", "net"))
	return err1 == nil && err2 == nil && theirs != ours
}

// readNetDev sums the byte counters of the namespace's interfaces except
// loopback.
func (f FS) readNetDev(pid int) ([2]uint64, error) {
	data, err := os.ReadFile(f.path(pid, "net", "dev"))
	if err != nil {
		return [2]uint64{}, err
	}
	var rx, tx uint64
	for _, line := range strings.Split(string(data), "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(name) == "lo" {
			continue
		}
		fl := strings.Fields(rest)
		if len(fl) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(fl[0], 10, 64)
		t, _ := strconv.ParseUint(fl[8], 10, 64)
		rx += r
		tx += t
	}
	return [2]uint64{rx, tx}, nil
}

// List returns the processes this user can identify, newest first. With
// onlyGo, only processes whose executable carries Go build information
// (executables of other users cannot be read, so their processes are left
// out; they can still be inspected by PID, with fewer metrics).
func (f FS) List(onlyGo bool) ([]*Process, error) {
	entries, err := os.ReadDir(f.Root)
	if err != nil {
		return nil, err
	}
	uid := os.Getuid()
	lc := listenCache{}
	var out []*Process
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		exeLink := f.path(pid, "exe")
		target, err := os.Readlink(exeLink)
		if onlyGo && (err != nil || goBuildInfo(exeLink, strings.TrimSuffix(target, " (deleted)")) == nil) {
			continue
		}
		p, err := f.inspect(pid, lc)
		if err != nil || !onlyGo && p.UID != uid && !p.Access.Exe {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartTicks > out[j].StartTicks })
	return out, nil
}

// FindSuccessor looks for a process running the same executable, started
// after the given one (a restart of the monitored program); the earliest
// such process wins.
func (f FS) FindSuccessor(prev *Process) *Process {
	if prev.Exe == "" {
		return nil
	}
	entries, err := os.ReadDir(f.Root)
	if err != nil {
		return nil
	}
	best, bestStart := 0, uint64(0)
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		target, err := os.Readlink(f.path(pid, "exe"))
		if err != nil || strings.TrimSuffix(target, " (deleted)") != prev.Exe {
			continue
		}
		st, err := f.readStat(pid)
		if err != nil || st.startTicks <= prev.StartTicks || st.state == "Z" {
			continue
		}
		if best == 0 || st.startTicks < bestStart {
			best, bestStart = pid, st.startTicks
		}
	}
	if best == 0 {
		return nil
	}
	p, err := f.Inspect(best)
	if err != nil {
		return nil
	}
	return p
}

// SameCmdline reports whether other (os.Args joined by NUL bytes, as
// net/http/pprof's cmdline endpoint serves it) is the command line of the
// process. Neither is kept: command lines may carry secrets.
func (f FS) SameCmdline(pid int, other []byte) (bool, error) {
	data, err := os.ReadFile(f.path(pid, "cmdline"))
	if err != nil {
		return false, err
	}
	return bytes.Equal(bytes.TrimRight(data, "\x00"), bytes.TrimRight(other, "\x00")), nil
}
