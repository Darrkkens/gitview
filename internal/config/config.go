// Package config loads optional per-repository settings from .gitview.json.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/athos/gitview/internal/testrunner"
)

const FileName = ".gitview.json"

// Config example:
//
//	{
//	  "exportDir": "reports",
//	  "tests": {
//	    "timeout": "5m",
//	    "projects": [
//	      {"name": "api", "kind": "go", "dir": "backend"},
//	      {"name": "e2e", "kind": "command", "dir": "web", "command": ["npm", "run", "test:e2e"]}
//	    ]
//	  },
//	  "runtime": {"maxCaptures": 20, "maxStorageMB": 200, "maxDuration": "5m", "interval": "500ms"}
//	}
type Config struct {
	ExportDir string `json:"exportDir"`
	Tests     struct {
		Timeout  string                     `json:"timeout"`
		Projects []testrunner.ProjectConfig `json:"projects"`
	} `json:"tests"`
	// Runtime limits the Runtime Performance Inspector. The repository is
	// not trusted: these settings can only tighten GitView's own limits.
	Runtime RuntimeConfig `json:"runtime"`
}

// RuntimeConfig is the "runtime" section of .gitview.json.
type RuntimeConfig struct {
	Disabled     bool   `json:"disabled"`     // turn the runtime inspector off for this repository
	MaxCaptures  int    `json:"maxCaptures"`  // captures kept in the history
	MaxStorageMB int    `json:"maxStorageMB"` // total size of the history
	MaxDuration  string `json:"maxDuration"`  // longest active phase
	Interval     string `json:"interval"`     // default sampling interval
}

// Load reads root/.gitview.json; a missing file yields the zero Config.
func Load(root string) (Config, error) {
	var c Config
	data, err := os.ReadFile(filepath.Join(root, FileName))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("%s: %w", FileName, err)
	}
	return c, nil
}

// TestTimeout parses tests.timeout, returning 0 when unset.
func (c Config) TestTimeout() (time.Duration, error) {
	if c.Tests.Timeout == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(c.Tests.Timeout)
	if err != nil {
		return 0, fmt.Errorf("%s: tests.timeout: %w", FileName, err)
	}
	return d, nil
}
