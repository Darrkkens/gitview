package audit

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/athos/gitview/internal/evidence"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// osvEntry is the part of an OSV advisory (Go vulnerability database
// format) the dependency rule reads.
type osvEntry struct {
	ID        string     `json:"id"`
	Aliases   []string   `json:"aliases"`
	Summary   string     `json:"summary"`
	Details   string     `json:"details"`
	Modified  time.Time  `json:"modified"`
	Withdrawn *time.Time `json:"withdrawn"`
	Affected  []struct {
		Package struct {
			Name      string `json:"name"`
			Ecosystem string `json:"ecosystem"`
		} `json:"package"`
		Ranges []struct {
			Type   string `json:"type"`
			Events []struct {
				Introduced string `json:"introduced"`
				Fixed      string `json:"fixed"`
			} `json:"events"`
		} `json:"ranges"`
		EcosystemSpecific struct {
			Imports []struct {
				Path    string   `json:"path"`
				Symbols []string `json:"symbols"`
			} `json:"imports"`
		} `json:"ecosystem_specific"`
	} `json:"affected"`
	References []struct {
		URL string `json:"url"`
	} `json:"references"`
}

// staleAfter: a database older than this is reported as possibly out of
// date.
const staleAfter = 30 * 24 * time.Hour

// loadOSV reads every advisory of a local database directory (the layout
// of the Go vulnerability database: ID/*.json, index/db.json) and the
// date the database was last modified.
func loadOSV(dir string) ([]osvEntry, time.Time, error) {
	var out []osvEntry
	var modified time.Time
	if data, err := os.ReadFile(filepath.Join(dir, "index", "db.json")); err == nil {
		var idx struct {
			Modified time.Time `json:"modified"`
		}
		if json.Unmarshal(data, &idx) == nil {
			modified = idx.Modified
		}
	}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "index" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var e osvEntry
		if json.Unmarshal(data, &e) != nil || e.ID == "" || len(e.Affected) == 0 {
			return nil // not an advisory
		}
		out = append(out, e)
		if e.Modified.After(modified) && modified.IsZero() {
			modified = e.Modified
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, modified, err
}

func canon(v string) string {
	if v == "" || v == "0" {
		return v
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

// affected reports whether version v is in one of the SEMVER ranges, and
// the first fixed version above it.
func affected(ranges []struct {
	Type   string `json:"type"`
	Events []struct {
		Introduced string `json:"introduced"`
		Fixed      string `json:"fixed"`
	} `json:"events"`
}, v string) (bool, string) {
	v = canon(v)
	for _, r := range ranges {
		if r.Type != "SEMVER" {
			continue
		}
		in, fixed := false, ""
		for _, ev := range r.Events {
			switch {
			case ev.Introduced != "":
				if ev.Introduced == "0" || semver.Compare(v, canon(ev.Introduced)) >= 0 {
					in = true
				}
			case ev.Fixed != "":
				if in && semver.Compare(v, canon(ev.Fixed)) >= 0 {
					in = false
				} else if in && fixed == "" {
					fixed = ev.Fixed
				}
			}
		}
		if in {
			return true, fixed
		}
	}
	return false, ""
}

func ruleDependencies(s *scanner, r *rule) {
	st := &s.audit.Dependencies
	if s.opt.VulnDB == "" {
		st.Note = "No vulnerability database configured: dependencies were not checked. Pass --vulndb with a local copy of the Go vulnerability database (OSV format)."
		s.status.Limit("dependencies not checked (no vulnerability database configured)")
		return
	}
	entries, modified, err := loadOSV(s.opt.VulnDB)
	st.Database, st.Modified = s.opt.VulnDB, modified
	if err != nil {
		st.Note = "The vulnerability database could not be read: " + err.Error()
		s.status.Limit("dependencies not checked: " + err.Error())
		return
	}
	st.Checked = true
	if modified.IsZero() || s.opt.Now().Sub(modified) > staleAfter {
		st.Stale = true
		st.Note = "The vulnerability database is older than 30 days (or has no date): advisories published since then are not covered."
		s.status.Limit("vulnerability database possibly out of date")
	}
	// External functions the module calls, for the advisories that list
	// vulnerable symbols.
	called := map[string]bool{}
	s.eachCall(func(c callInfo) { called[c.name] = true })
	for _, p := range s.in.Programs {
		rel := strings.TrimPrefix(p.RelFile(filepath.Join(p.Dir, "go.mod")), "./")
		data, err := os.ReadFile(filepath.Join(p.Dir, "go.mod"))
		if err != nil {
			continue
		}
		mf, err := modfile.Parse(rel, data, nil)
		if err != nil {
			s.status.Limit(rel + ": " + err.Error())
			continue
		}
		replaced := map[string]string{}
		for _, rp := range mf.Replace {
			replaced[rp.Old.Path] = rp.New.Path + " " + rp.New.Version
		}
		for _, req := range mf.Require {
			st.Modules++
			for _, e := range entries {
				if e.Withdrawn != nil {
					continue
				}
				for _, a := range e.Affected {
					if a.Package.Name != req.Mod.Path {
						continue
					}
					hit, fixed := affected(a.Ranges, req.Mod.Version)
					if !hit {
						continue
					}
					line := 0
					if req.Syntax != nil {
						line = req.Syntax.Start.Line
					}
					f := s.newFinding(r, nil, nil, rel, line, e.ID+" "+req.Mod.Path)
					f.File, f.Line = rel, line
					f.Title = e.ID + ": " + firstNonEmpty(e.Summary, r.Title)
					f.Confidence, f.Verdict = ConfLikely, Review
					f.Message = req.Mod.Path + " " + req.Mod.Version + " is affected by " + e.ID
					if fixed != "" {
						f.Message += " (fixed in " + fixed + ")"
						f.Remediation = append([]string{"go get " + req.Mod.Path + "@v" + strings.TrimPrefix(fixed, "v")}, f.Remediation...)
					}
					f.Message += "."
					f.ev(Evidence{Kind: EvDependency, Text: "go.mod requires " + req.Mod.Path + " " + req.Mod.Version, File: rel, Line: line, Code: strings.TrimSpace(sourceLine(data, line)), Confidence: evidence.Exact, Source: "go.mod"})
					f.ev(Evidence{Kind: EvDependency, Text: e.ID + firstAlias(e.Aliases) + ": " + firstNonEmpty(e.Summary, e.Details), Source: "osv:" + filepath.Base(s.opt.VulnDB), Basis: "database"})
					if rp, ok := replaced[req.Mod.Path]; ok {
						f.limit("the module is replaced by " + strings.TrimSpace(rp) + "; the advisory is matched against the required version")
					}
					var symbols []string
					for _, imp := range a.EcosystemSpecific.Imports {
						for _, sym := range imp.Symbols {
							symbols = append(symbols, imp.Path+"."+sym)
						}
					}
					if len(symbols) > 0 {
						var hits []string
						for _, sym := range symbols {
							for name := range called {
								if strings.HasSuffix(name, sym) || strings.Contains(name, sym+")") || name == sym {
									hits = append(hits, name)
								}
							}
						}
						if len(hits) > 0 {
							sort.Strings(hits)
							f.Confidence = ConfConfirmed
							f.Message += " The module calls an affected symbol: " + strings.Join(dedupe(hits), ", ") + "."
							f.ev(Evidence{Kind: EvSink, Text: "calls " + strings.Join(dedupe(hits), ", "), Source: "go/ssa"})
						} else {
							f.Confidence = ConfPossible
							f.Message += " None of the affected symbols listed by the advisory is called by the analysed code."
						}
					} else {
						f.limit("the advisory lists no symbols: reachability is not checked")
					}
					if st.Stale {
						f.limit("the vulnerability database may be out of date")
					}
					for _, ref := range e.References {
						f.Remediation = append(f.Remediation, "Advisory: "+ref.URL)
						break
					}
					s.add(f)
				}
			}
		}
	}
}

func sourceLine(data []byte, line int) string {
	lines := strings.Split(string(data), "\n")
	if line > 0 && line <= len(lines) {
		return lines[line-1]
	}
	return ""
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return strings.TrimSpace(x)
		}
	}
	return ""
}

func firstAlias(a []string) string {
	if len(a) == 0 {
		return ""
	}
	return " (" + strings.Join(a, ", ") + ")"
}
