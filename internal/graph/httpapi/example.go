package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/athos/gitview/internal/graph/model"
)

// Synthetic values: recognisable placeholders, never data read from the
// application or its database.
const (
	exampleString = "example"
	exampleEmail  = "user@example.com"
	exampleURL    = "https://example.com"
	exampleUUID   = "00000000-0000-4000-8000-000000000000"
	exampleIP     = "192.0.2.1" // TEST-NET-1, reserved for documentation
	exampleE164   = "+15555550100"
)

var exampleTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// ordered is a JSON object that keeps the declaration order of its keys.
type ordered []kv

type kv struct {
	k string
	v any
}

func (o ordered) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, x := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(x.k)
		v, err := json.Marshal(x.v)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// gen builds example values and records what it could not guarantee.
type gen struct {
	defs     map[string]*model.Shape
	enforced []string
	notes    []string
	unknown  map[string]bool // rules it does not understand
	seen     map[string]int
}

// ExampleValue builds a synthetic value of a shape. enforced lists the tag
// families whose rules the decoding applies; rules of other families are
// still honoured when possible. The returned notes say what the value may
// not satisfy.
func ExampleValue(defs map[string]*model.Shape, sh *model.Shape, enforced []string) (any, string, []string) {
	g := &gen{defs: defs, enforced: enforced, unknown: map[string]bool{}, seen: map[string]int{}}
	v := g.value(sh, nil, 0)
	valid := "compatible"
	if len(g.unknown) > 0 {
		var names []string
		for n := range g.unknown {
			names = append(names, n)
		}
		sort.Strings(names)
		g.notes = append(g.notes, "validation rules not interpreted: "+strings.Join(names, ", "))
		valid = "unverified"
	}
	if len(g.notes) > 0 {
		valid = "unverified"
	}
	return v, valid, g.notes
}

func rule(rules []model.Rule, name string) (string, bool) {
	for _, r := range rules {
		if r.Name == name {
			return r.Param, true
		}
	}
	return "", false
}

func (g *gen) value(sh *model.Shape, rules []model.Rule, depth int) any {
	if sh == nil {
		return nil
	}
	if depth > 8 {
		g.notes = append(g.notes, "nested values beyond 8 levels are left null")
		return nil
	}
	for _, r := range rules {
		if !r.Known {
			g.unknown[r.Source+":"+r.Name] = true
		}
	}
	if sh.Ref != "" {
		if g.seen[sh.Ref] > 1 {
			return nil // recursive type: stop after one repetition
		}
		g.seen[sh.Ref]++
		defer func() { g.seen[sh.Ref]-- }()
		sh = resolve(g.defs, sh)
	}
	if p, ok := rule(rules, "oneof"); ok && p != "" {
		first := strings.Fields(p)[0]
		switch sh.Type {
		case model.TypeInteger:
			if n, err := strconv.ParseInt(first, 10, 64); err == nil {
				return n
			}
		case model.TypeNumber:
			if n, err := strconv.ParseFloat(first, 64); err == nil {
				return n
			}
		}
		return first
	}
	switch sh.Type {
	case model.TypeObject:
		if sh.Values != nil && len(sh.Fields) == 0 {
			return ordered{{"key", g.value(sh.Values, nil, depth+1)}}
		}
		out := ordered{}
		for _, f := range sh.Fields {
			if f.Conditional {
				continue
			}
			out = append(out, kv{f.Name, g.value(f.Schema, f.Rules, depth+1)})
		}
		if sh.Partial {
			g.notes = append(g.notes, describeShape(sh)+": "+sh.Note)
		}
		return out
	case model.TypeArray:
		n := 1
		if p, ok := rule(rules, "min"); ok {
			if m, err := strconv.Atoi(p); err == nil && m > n {
				n = min(m, 5)
			}
		}
		var item []model.Rule
		if i := diveIndex(rules); i >= 0 {
			item = rules[i+1:]
		}
		out := make([]any, n)
		for i := range out {
			out[i] = g.value(sh.Items, item, depth+1)
		}
		return out
	case model.TypeString:
		return g.str(sh, rules)
	case model.TypeInteger:
		return int64(g.num(rules))
	case model.TypeNumber:
		return g.num(rules)
	case model.TypeBoolean:
		return false
	}
	g.notes = append(g.notes, "a value of unknown type ("+describeShape(sh)+") is left null")
	return nil
}

func diveIndex(rules []model.Rule) int {
	for i, r := range rules {
		if r.Name == "dive" {
			return i
		}
	}
	return -1
}

func (g *gen) str(sh *model.Shape, rules []model.Rule) string {
	s := exampleString
	switch {
	case has(rules, "email"):
		s = exampleEmail
	case has(rules, "url") || has(rules, "uri"):
		s = exampleURL
	case has(rules, "uuid") || has(rules, "uuid4") || sh.Format == "uuid":
		s = exampleUUID
	case has(rules, "ip") || has(rules, "ipv4"):
		s = exampleIP
	case has(rules, "e164"):
		s = exampleE164
	case has(rules, "numeric"):
		s = "123"
	case has(rules, "alphanum"):
		s = "example1"
	case sh.Format == "date-time":
		s = exampleTime.Format(time.RFC3339)
	case sh.Format == "byte":
		s = "ZXhhbXBsZQ==" // base64("example")
	case sh.Format == "binary":
		s = "<file>"
	}
	if layout, ok := rule(rules, "datetime"); ok && layout != "" {
		s = exampleTime.Format(layout)
	}
	if p, ok := rule(rules, "len"); ok {
		if n, err := strconv.Atoi(p); err == nil {
			return fit(s, n, n)
		}
	}
	lo, hi := 0, -1
	if p, ok := rule(rules, "min"); ok {
		lo, _ = strconv.Atoi(p)
	}
	if p, ok := rule(rules, "gte"); ok {
		lo, _ = strconv.Atoi(p)
	}
	if p, ok := rule(rules, "max"); ok {
		hi, _ = strconv.Atoi(p)
	}
	if p, ok := rule(rules, "lte"); ok {
		hi, _ = strconv.Atoi(p)
	}
	return fit(s, lo, hi)
}

// fit pads or cuts s to a length between lo and hi (hi < 0: no limit).
func fit(s string, lo, hi int) string {
	for len(s) < lo {
		s += "x"
	}
	if hi >= 0 && len(s) > hi {
		s = s[:hi]
	}
	return s
}

func has(rules []model.Rule, name string) bool {
	_, ok := rule(rules, name)
	return ok
}

// num chooses the smallest value the bounds allow, 0 when unbounded.
func (g *gen) num(rules []model.Rule) float64 {
	v := 0.0
	parse := func(name string) (float64, bool) {
		p, ok := rule(rules, name)
		if !ok {
			return 0, false
		}
		f, err := strconv.ParseFloat(p, 64)
		return f, err == nil
	}
	if f, ok := parse("min"); ok && f > v {
		v = f
	}
	if f, ok := parse("gte"); ok && f > v {
		v = f
	}
	if f, ok := parse("gt"); ok && f >= v {
		v = f + 1
	}
	if f, ok := parse("max"); ok && v > f {
		v = f
	}
	if f, ok := parse("lte"); ok && v > f {
		v = f
	}
	if f, ok := parse("lt"); ok && v >= f {
		v = f - 1
	}
	return v
}

// ExampleParam is a synthetic value for a parameter.
func ExampleParam(p model.Param) string {
	if p.Default != "" {
		return p.Default
	}
	if p.Schema != nil {
		switch p.Schema.Type {
		case model.TypeInteger, model.TypeNumber:
			return "1"
		case model.TypeBoolean:
			return "true"
		}
		if p.Schema.Format == "uuid" {
			return exampleUUID
		}
	}
	return exampleString
}

// Indent formats a value as indented JSON.
func Indent(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
