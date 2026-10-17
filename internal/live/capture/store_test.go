package capture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/athos/gitview/internal/live/goprof"
)

func TestEnforceSizeDropsLargestArtifactsFirst(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, n int) {
		os.WriteFile(filepath.Join(dir, name), make([]byte, n), 0o600)
	}
	write("trace.out", 5000)
	write("heap-start.pb.gz", 100)
	write("cpu-001.pb.gz", 100)
	c := &Capture{Dir: dir, Profiles: &goprof.Result{
		Trace:     &goprof.Window{File: "trace.out"},
		HeapStart: &goprof.Window{File: "heap-start.pb.gz"},
		CPU:       []goprof.Window{{File: "cpu-001.pb.gz"}},
	}}
	dropped := enforceSize(c, 1000)
	if strings.Join(dropped, ",") != "the execution trace" || c.Profiles.Trace.File != "" || c.Profiles.HeapStart.File == "" || c.Profiles.CPU[0].File == "" {
		t.Errorf("dropped %v, trace %+v", dropped, c.Profiles.Trace)
	}
	if _, err := os.Stat(filepath.Join(dir, "trace.out")); !os.IsNotExist(err) {
		t.Error("trace file kept")
	}
	if enforceSize(c, 1<<20) != nil {
		t.Error("removed artifacts under the limit")
	}
}
