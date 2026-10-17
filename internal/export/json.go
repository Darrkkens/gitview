package export

import (
	"encoding/json"
	"time"

	"github.com/athos/gitview/internal/gitdiff"
	"github.com/athos/gitview/internal/study"
)

const schemaVersion = 1

type jsonReport struct {
	SchemaVersion int          `json:"schemaVersion"`
	GeneratedAt   time.Time    `json:"generatedAt"`
	Repository    jsonRepo     `json:"repository"`
	Scope         string       `json:"scope,omitempty"`
	Query         string       `json:"query,omitempty"`
	Summary       jsonSummary  `json:"summary"`
	Files         []jsonFile   `json:"files"`
	Tests         TestReport   `json:"tests"`
	Warnings      []string     `json:"warnings"`
	CodeFlow      *FlowData    `json:"codeFlow,omitempty"`
	Inspector     *InspectData `json:"inspector,omitempty"`
	Study         *study.Model `json:"study,omitempty"`
}

type jsonRepo struct {
	Name   string `json:"name"`
	Root   string `json:"root"`
	Branch string `json:"branch"`
}

type jsonSummary struct {
	Files   int `json:"files"`
	Added   int `json:"added"`
	Deleted int `json:"deleted"`
}

type jsonFile struct {
	gitdiff.File
	ContentOmitted bool `json:"contentOmitted,omitempty"`
}

func renderJSON(r Report) ([]byte, error) {
	added, deleted := r.totals()
	out := jsonReport{
		SchemaVersion: schemaVersion,
		GeneratedAt:   r.GeneratedAt,
		Repository:    jsonRepo{r.Project, r.Root, r.Branch},
		Scope:         r.Scope,
		Query:         r.Query,
		Summary:       jsonSummary{len(r.Files), added, deleted},
		Files:         make([]jsonFile, 0, len(r.Files)),
		Warnings:      append([]string{}, r.Warnings...),
	}
	for _, f := range r.Files {
		jf := jsonFile{File: f, ContentOmitted: r.omitted(f.Path)}
		if jf.Hunks == nil {
			jf.Hunks = []gitdiff.Hunk{}
		}
		out.Files = append(out.Files, jf)
	}
	if r.Tests != nil {
		out.Tests = *r.Tests
	}
	out.CodeFlow = r.Flow
	out.Inspector = r.Inspect
	out.Study = r.Study
	for _, o := range r.Omitted {
		out.Warnings = append(out.Warnings, "conteúdo omitido (arquivo potencialmente sensível): "+o)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
