// Package audit is CodeAudit's static security analyzer: deterministic
// rules over the analyses GitView already has (Code Flow call sites and
// routes, Data Flow traces, the SQL Explorer, the HTTP catalog of the
// Repository Graph). Every finding points to evidence in the code; nothing
// here executes the analysed program or calls a model. The AI investigator
// (package audit/ai) only reads findings, it never creates or confirms one.
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/athos/gitview/internal/evidence"
)

// Versions written into every audit, so a snapshot says what produced it.
const (
	Schema       = "gitview.audit/v1"
	RulesVersion = "2026.10.1"
)

// Severity: the impact if the finding is a real vulnerability.
const (
	SevCritical = "critical"
	SevHigh     = "high"
	SevMedium   = "medium"
	SevLow      = "low"
	SevInfo     = "info"
)

// Confidence: how strongly the static evidence supports the finding.
const (
	ConfConfirmed = "confirmed" // the evidence is direct (a constant, an exact data path)
	ConfLikely    = "likely"
	ConfPossible  = "possible"
)

// Verdict: what the analysis concluded, shown as the colour of the graph.
const (
	Vulnerable = "vulnerable" // red: the evidence is sufficient on its own
	Review     = "review"     // yellow: possible risk, a person has to decide
	Incomplete = "incomplete" // grey: the analysis could not finish the path
)

// Review status: the decision of a person, never of the analysis or the AI.
const (
	StatusOpen              = "open"
	StatusInvestigating     = "investigating"
	StatusConfirmed         = "confirmed"
	StatusFalsePositive     = "false_positive"
	StatusAcceptedRisk      = "accepted_risk"
	StatusFixed             = "fixed"
	StatusNeedsRevalidation = "needs_revalidation"
)

// Statuses lists the review statuses in workflow order.
var Statuses = []string{StatusOpen, StatusInvestigating, StatusConfirmed, StatusFalsePositive, StatusAcceptedRisk, StatusFixed, StatusNeedsRevalidation}

// ValidStatus reports whether s is a review status.
func ValidStatus(s string) bool {
	for _, x := range Statuses {
		if x == s {
			return true
		}
	}
	return false
}

// Categories.
const (
	CatAuthentication = "authentication"
	CatAuthorization  = "authorization"
	CatInjection      = "injection"
	CatSensitiveData  = "sensitive-data"
	CatConfiguration  = "configuration"
	CatDependencies   = "dependencies"
)

// Evidence kinds of a finding.
const (
	EvSource       = "source"      // untrusted data enters (HTTP input)
	EvPropagation  = "propagation" // the value moves (assignment, argument, return)
	EvTransform    = "transformation"
	EvSanitizer    = "sanitizer"    // a recognised neutralisation
	EvValidation   = "validation"   // a condition on the value
	EvSink         = "sink"         // the sensitive operation
	EvCheck        = "check"        // a security check on the path (authentication, authorization)
	EvAbsence      = "absence"      // something expected was not found (stated as a limit, never as proof)
	EvConfig       = "config"       // a setting
	EvRegistration = "registration" // where a route is registered
	EvDependency   = "dependency"
)

// Evidence is one verifiable fact behind a finding.
type Evidence struct {
	ID         string `json:"id"` // ev-…, stable within the finding
	Kind       string `json:"kind"`
	Basis      string `json:"basis"` // static | heuristic
	Text       string `json:"text"`
	File       string `json:"file,omitempty"`
	Line       int    `json:"line,omitempty"`
	EndLine    int    `json:"endLine,omitempty"`
	Symbol     string `json:"symbol,omitempty"` // graph node ID of the function
	Code       string `json:"code,omitempty"`   // the source line, secrets redacted
	Confidence string `json:"confidence,omitempty"`
	Source     string `json:"source"` // analysis that produced it: go/ssa, go/types, dataflow, sqlops, httpapi, osv
}

// FlowStep is one step of the data path from an input to the sensitive
// operation, as Data Flow found it.
type FlowStep struct {
	Role  string `json:"role"` // source | propagation | transformation | sanitizer | validation | sink
	Label string `json:"label"`
	Func  string `json:"func,omitempty"`
	File  string `json:"file,omitempty"`
	Line  int    `json:"line,omitempty"`
	Code  string `json:"code,omitempty"`
	Conf  string `json:"conf,omitempty"` // confidence of the edge into this step
	Via   string `json:"via,omitempty"`  // how the value got here (argument of X, returned by Y)
}

// PathStep is one element of the execution path from a route to the
// finding: the route, its chain, the functions called on the way.
type PathStep struct {
	ID    string `json:"id"` // Repository Graph node ID
	Name  string `json:"name"`
	Role  string `json:"role"` // route | middleware | handler | function | sink
	File  string `json:"file,omitempty"`
	Line  int    `json:"line,omitempty"`
	Conf  string `json:"conf,omitempty"`  // confidence of the call into this step
	Check string `json:"check,omitempty"` // security check identified at this step
}

// RouteRef ties a finding to a route that may reach it.
type RouteRef struct {
	ID   string     `json:"id"`
	Key  string     `json:"key"`
	Auth string     `json:"auth"` // how authentication was recognised, or "none identified"
	Path []PathStep `json:"path"`
}

// Check is a security control found by the analysis (green in the graph).
type Check struct {
	Kind   string `json:"kind"` // authentication | authorization | tenant-filter | parameterized | sanitizer | validation
	Label  string `json:"label"`
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Symbol string `json:"symbol,omitempty"`
	Basis  string `json:"basis"`
}

// Finding is a potential security problem and its evidence.
type Finding struct {
	ID           string `json:"id"`
	RuleID       string `json:"ruleId"`
	Title        string `json:"title"`
	Category     string `json:"category"`
	Severity     string `json:"severity"`
	Confidence   string `json:"confidence"`
	Verdict      string `json:"verdict"`
	VerdictLabel string `json:"verdictLabel"` // e.g. "Review Required — Authorization Not Established"
	Origin       string `json:"origin"`       // static-rule: the only origin of findings

	Message     string   `json:"message"` // what was found, specifically
	Why         string   `json:"why"`     // why it can be a problem
	Remediation []string `json:"remediation"`
	Tests       []string `json:"tests"`

	Symbol   *evidence.SymbolRef `json:"symbol,omitempty"` // function containing the sink
	SymbolID string              `json:"symbolId,omitempty"`
	File     string              `json:"file,omitempty"`
	Line     int                 `json:"line,omitempty"`
	EndLine  int                 `json:"endLine,omitempty"`

	Routes     []RouteRef `json:"routes,omitempty"`
	Evidence   []Evidence `json:"evidence"`
	Flow       []FlowStep `json:"flow,omitempty"`
	Checks     []Check    `json:"checks,omitempty"`
	Unresolved []string   `json:"unresolved,omitempty"` // relations the analysis could not determine

	AnalysisStatus string   `json:"analysisStatus"` // complete | partial
	Limits         []string `json:"limits,omitempty"`

	// Anchor is what the ID is derived from: rule, symbol, sensitive call
	// and its normalised code, never a line number. CodeHash changes when
	// the code at the finding changes (a reason to revalidate a review).
	Anchor   string `json:"anchor"`
	CodeHash string `json:"codeHash"`
}

// Audit is the result of a scan.
type Audit struct {
	Schema          string                `json:"schema"`
	RulesVersion    string                `json:"rulesVersion"`
	AnalyzerVersion string                `json:"analyzerVersion"`
	Repository      string                `json:"repository"`
	Commit          string                `json:"commit,omitempty"`
	Dirty           bool                  `json:"dirty,omitempty"` // uncommitted changes were analysed
	Generated       time.Time             `json:"generated"`
	Modules         []string              `json:"modules"`
	Status          evidence.Completeness `json:"status"`
	Rules           []RuleInfo            `json:"rules"`
	Findings        []*Finding            `json:"findings"`
	Routes          []RoutePosture        `json:"routes"`
	Dependencies    DependencyStatus      `json:"dependencies"`
	DurationMS      int64                 `json:"durationMs"`
}

// RoutePosture summarises what the analysis found for one route.
type RoutePosture struct {
	ID         string   `json:"id"`
	Key        string   `json:"key"`
	Auth       string   `json:"auth"`
	Checks     []Check  `json:"checks,omitempty"`
	Findings   []string `json:"findings,omitempty"`   // IDs
	Unresolved int      `json:"unresolved,omitempty"` // calls without a static target in the chain
	DynamicSQL int      `json:"dynamicSql,omitempty"` // reached database operations with SQL built at runtime
}

// DependencyStatus says whether and how dependencies were checked.
type DependencyStatus struct {
	Checked  bool      `json:"checked"`
	Database string    `json:"database,omitempty"` // local path of the OSV database (never sent anywhere)
	Modified time.Time `json:"modified,omitzero"`
	Stale    bool      `json:"stale,omitempty"`
	Modules  int       `json:"modules"`
	Note     string    `json:"note,omitempty"`
}

var itoa = strconv.Itoa

// stableID derives an identifier from the given parts.
func stableID(prefix string, parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + hex.EncodeToString(h[:6])
}

// normCode collapses whitespace so that formatting does not change IDs.
func normCode(s string) string { return strings.Join(strings.Fields(s), " ") }

// Finding returns the finding with the given ID.
func (a *Audit) Finding(id string) *Finding {
	for _, f := range a.Findings {
		if f.ID == id {
			return f
		}
	}
	return nil
}
