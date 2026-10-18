package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Store keeps the audits of one repository outside it, in GitView's cache
// directory (GITVIEW_CACHE_DIR overrides, as for runtime captures):
//
//	snapshots/<time>-<commit>.json   every scan, for comparisons
//	reviews.json                     the decisions of reviewers, with history
//	ai/<hash>.json                   AI investigations, by context and model
//
// Files are written atomically and readable only by the user.
type Store struct {
	Dir string
}

// StoreFor returns the store of a repository root; dir, when set, is used
// instead (for example to keep reviews next to the code).
func StoreFor(repoRoot, dir string) (*Store, error) {
	if dir != "" {
		return &Store{Dir: dir}, nil
	}
	base := os.Getenv("GITVIEW_CACHE_DIR")
	if base == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return nil, err
		}
		base = filepath.Join(cache, "gitview")
	}
	sum := sha256.Sum256([]byte(repoRoot))
	return &Store{Dir: filepath.Join(base, hex.EncodeToString(sum[:6]), "audit")}, nil
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// ---------- snapshots ----------

// SnapshotInfo identifies a saved audit.
type SnapshotInfo struct {
	ID        string    `json:"id"`
	Commit    string    `json:"commit,omitempty"`
	Dirty     bool      `json:"dirty,omitempty"`
	Generated time.Time `json:"generated"`
	Findings  int       `json:"findings"`
	Status    string    `json:"status"`
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	if c == "" {
		return "nocommit"
	}
	return c
}

// Save writes a snapshot of the audit and returns its ID.
func (s *Store) Save(a *Audit) (string, error) {
	id := a.Generated.UTC().Format("20060102-150405") + "-" + shortCommit(a.Commit)
	if a.Dirty {
		id += "-dirty"
	}
	return id, writeJSON(filepath.Join(s.Dir, "snapshots", id+".json"), a)
}

// Snapshots lists the saved audits, oldest first.
func (s *Store) Snapshots() ([]SnapshotInfo, error) {
	entries, err := os.ReadDir(filepath.Join(s.Dir, "snapshots"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []SnapshotInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		var a Audit
		if err := readJSON(filepath.Join(s.Dir, "snapshots", e.Name()), &a); err != nil || a.Schema != Schema {
			continue
		}
		out = append(out, SnapshotInfo{ID: id, Commit: a.Commit, Dirty: a.Dirty, Generated: a.Generated, Findings: len(a.Findings), Status: string(a.Status.Status)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Load reads a snapshot by ID, unique ID prefix, or commit prefix
// (the latest snapshot of that commit).
func (s *Store) Load(ref string) (*Audit, string, error) {
	list, err := s.Snapshots()
	if err != nil {
		return nil, "", err
	}
	var match []SnapshotInfo
	for _, sn := range list {
		if sn.ID == ref || strings.HasPrefix(sn.ID, ref) || (len(ref) >= 4 && strings.HasPrefix(sn.Commit, ref)) {
			match = append(match, sn)
		}
	}
	if len(match) == 0 {
		return nil, "", fmt.Errorf("no saved audit matches %q", ref)
	}
	id := match[len(match)-1].ID
	var a Audit
	if err := readJSON(filepath.Join(s.Dir, "snapshots", id+".json"), &a); err != nil {
		return nil, "", err
	}
	return &a, id, nil
}

// Latest returns the most recent snapshot, and the one before it.
func (s *Store) Latest() (cur, prev *Audit, err error) {
	list, err := s.Snapshots()
	if err != nil || len(list) == 0 {
		return nil, nil, err
	}
	cur, _, err = s.Load(list[len(list)-1].ID)
	if err != nil {
		return nil, nil, err
	}
	if len(list) > 1 {
		prev, _, _ = s.Load(list[len(list)-2].ID)
	}
	return cur, prev, nil
}

// ---------- reviews ----------

// Decision is one change of a finding's review status.
type Decision struct {
	Time          time.Time `json:"time"`
	From          string    `json:"from"`
	To            string    `json:"to"`
	Justification string    `json:"justification"`
	Commit        string    `json:"commit,omitempty"` // commit of the audit the reviewer looked at
	CodeHash      string    `json:"codeHash"`         // code of the finding when decided
	By            string    `json:"by"`               // "reviewer"; the AI never decides
}

// ReviewRecord is the review state of one finding.
type ReviewRecord struct {
	FindingID string     `json:"findingId"`
	RuleID    string     `json:"ruleId"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	History   []Decision `json:"history"`
}

// Reviews are the decisions on the findings of a repository.
type Reviews struct {
	Schema   string                   `json:"schema"`
	Findings map[string]*ReviewRecord `json:"findings"`
}

const reviewsSchema = "gitview.audit.reviews/v1"

func (s *Store) reviewsPath() string { return filepath.Join(s.Dir, "reviews.json") }

// Reviews reads the review decisions (empty when none were made).
func (s *Store) Reviews() (*Reviews, error) {
	r := &Reviews{Schema: reviewsSchema, Findings: map[string]*ReviewRecord{}}
	err := readJSON(s.reviewsPath(), r)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if r.Findings == nil {
		r.Findings = map[string]*ReviewRecord{}
	}
	return r, nil
}

// SaveReviews writes the decisions.
func (s *Store) SaveReviews(r *Reviews) error { return writeJSON(s.reviewsPath(), r) }

// statuses that close a finding need a reason.
var needsJustification = map[string]bool{StatusFalsePositive: true, StatusAcceptedRisk: true, StatusFixed: true, StatusConfirmed: true}

// Decide records a reviewer's decision.
func (r *Reviews) Decide(f *Finding, status, justification, commit string, now time.Time) (*ReviewRecord, error) {
	if !ValidStatus(status) {
		return nil, fmt.Errorf("unknown status %q (use one of %s)", status, strings.Join(Statuses, ", "))
	}
	justification = strings.TrimSpace(justification)
	if needsJustification[status] && justification == "" {
		return nil, fmt.Errorf("status %s needs a justification", status)
	}
	rec := r.Findings[f.ID]
	if rec == nil {
		rec = &ReviewRecord{FindingID: f.ID, RuleID: f.RuleID, Title: f.Title, Status: StatusOpen}
		r.Findings[f.ID] = rec
	}
	rec.History = append(rec.History, Decision{Time: now.UTC(), From: rec.Status, To: status, Justification: justification, Commit: commit, CodeHash: f.CodeHash, By: "reviewer"})
	rec.Status = status
	return rec, nil
}

// Revalidation explains why a decided finding should be looked at again:
// its code changed since the decision, or it was marked fixed and the
// analysis still finds it. Nothing changes the status automatically.
func (r *Reviews) Revalidation(f *Finding) string {
	rec := r.Findings[f.ID]
	if rec == nil || len(rec.History) == 0 {
		return ""
	}
	last := rec.History[len(rec.History)-1]
	switch {
	case rec.Status == StatusFixed:
		return "marked fixed, but the analysis still finds it"
	case rec.Status != StatusOpen && last.CodeHash != "" && last.CodeHash != f.CodeHash:
		return "the code changed after the decision"
	}
	return ""
}

// Status returns the review status of a finding (open when undecided).
func (r *Reviews) Status(id string) string {
	if rec := r.Findings[id]; rec != nil {
		return rec.Status
	}
	return StatusOpen
}

// ---------- AI investigations ----------

// SaveInvestigation stores an AI result under its cache key and as the
// latest investigation of its subject (a finding ID or a path).
func (s *Store) SaveInvestigation(key, subject string, v any) error {
	if err := writeJSON(filepath.Join(s.Dir, "ai", key+".json"), v); err != nil {
		return err
	}
	return writeJSON(filepath.Join(s.Dir, "ai", "latest", safeName(subject)+".json"), v)
}

// Investigation reads a cached AI result.
func (s *Store) Investigation(key string, v any) bool {
	return readJSON(filepath.Join(s.Dir, "ai", key+".json"), v) == nil
}

// LatestInvestigation reads the last AI result for a subject.
func (s *Store) LatestInvestigation(subject string, v any) bool {
	return readJSON(filepath.Join(s.Dir, "ai", "latest", safeName(subject)+".json"), v) == nil
}

func safeName(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:10])
}
