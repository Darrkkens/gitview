package perf

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"

	"github.com/athos/gitview/internal/pbwire"
)

// This file decodes the pprof protocol buffer format (profile.proto), as
// written by runtime/pprof and `go test -cpuprofile/-memprofile`. Only the
// fields needed for analysis are kept.

type ValueType struct {
	Type string `json:"type"`
	Unit string `json:"unit"`
}

type Line struct {
	Func *Function
	Line int64
}

type Location struct {
	ID    uint64
	Lines []Line // innermost (inlined callee) first
}

type Function struct {
	ID         uint64
	Name       string
	SystemName string
	File       string
	StartLine  int64
}

type Sample struct {
	Locs   []uint64 // leaf first
	Values []int64
	// Labels are the sample's string labels (pprof.Labels / pprof.Do).
	Labels map[string]string

	rawLabels [][2]int64 // key, str (string table indexes)
}

// Profile is a decoded pprof profile.
type Profile struct {
	SampleTypes   []ValueType
	DefaultType   string
	Samples       []Sample
	Locations     map[uint64]*Location
	Functions     map[uint64]*Function
	PeriodType    ValueType
	Period        int64
	TimeNanos     int64
	DurationNanos int64
	Comments      []string
}

var ErrNotProfile = errors.New("not a pprof profile (expected the gzip-compressed protocol buffer format)")

// Parse decodes a profile, compressed or not.
func Parse(data []byte) (*Profile, error) {
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, ErrNotProfile
		}
		raw, err := io.ReadAll(io.LimitReader(zr, 1<<30))
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrNotProfile, err)
		}
		data = raw
	}
	if len(data) == 0 {
		return nil, ErrNotProfile
	}
	p := &Profile{Locations: map[uint64]*Location{}, Functions: map[uint64]*Function{}}
	var strtab []string
	type rawFunc struct {
		f               *Function
		name, sys, file int64
	}
	var funcs []rawFunc
	type rawLoc struct {
		loc   *Location
		lines [][2]int64 // function id, line
	}
	var locs []rawLoc
	var sampleTypes [][2]int64
	var periodType [2]int64
	var comments []int64
	var defaultType int64
	err := fields(data, func(num int, wt int, v uint64, b []byte) error {
		switch num {
		case 1: // sample_type
			t, err := valueType(b)
			sampleTypes = append(sampleTypes, t)
			return err
		case 2: // sample
			s, err := sample(b)
			p.Samples = append(p.Samples, s)
			return err
		case 4: // location
			l := rawLoc{loc: &Location{}}
			err := fields(b, func(n, wt int, v uint64, b []byte) error {
				switch n {
				case 1:
					l.loc.ID = v
				case 4:
					var ln [2]int64
					err := fields(b, func(n, wt int, v uint64, _ []byte) error {
						switch n {
						case 1:
							ln[0] = int64(v)
						case 2:
							ln[1] = int64(v)
						}
						return nil
					})
					l.lines = append(l.lines, ln)
					return err
				}
				return nil
			})
			locs = append(locs, l)
			return err
		case 5: // function
			f := rawFunc{f: &Function{}}
			err := fields(b, func(n, wt int, v uint64, _ []byte) error {
				switch n {
				case 1:
					f.f.ID = v
				case 2:
					f.name = int64(v)
				case 3:
					f.sys = int64(v)
				case 4:
					f.file = int64(v)
				case 5:
					f.f.StartLine = int64(v)
				}
				return nil
			})
			funcs = append(funcs, f)
			return err
		case 6: // string_table
			strtab = append(strtab, string(b))
		case 9:
			p.TimeNanos = int64(v)
		case 10:
			p.DurationNanos = int64(v)
		case 11:
			t, err := valueType(b)
			periodType = t
			return err
		case 12:
			p.Period = int64(v)
		case 13:
			if wt == 2 {
				return packed(b, func(x uint64) { comments = append(comments, int64(x)) })
			}
			comments = append(comments, int64(v))
		case 14:
			defaultType = int64(v)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotProfile, err)
	}
	str := func(i int64) string {
		if i >= 0 && int(i) < len(strtab) {
			return strtab[i]
		}
		return ""
	}
	if len(strtab) == 0 || strtab[0] != "" || len(sampleTypes) == 0 {
		return nil, ErrNotProfile
	}
	for _, t := range sampleTypes {
		p.SampleTypes = append(p.SampleTypes, ValueType{str(t[0]), str(t[1])})
	}
	p.PeriodType = ValueType{str(periodType[0]), str(periodType[1])}
	p.DefaultType = str(defaultType)
	for _, c := range comments {
		p.Comments = append(p.Comments, str(c))
	}
	for _, f := range funcs {
		f.f.Name, f.f.SystemName, f.f.File = str(f.name), str(f.sys), str(f.file)
		p.Functions[f.f.ID] = f.f
	}
	for _, l := range locs {
		for _, ln := range l.lines {
			l.loc.Lines = append(l.loc.Lines, Line{Func: p.Functions[uint64(ln[0])], Line: ln[1]})
		}
		p.Locations[l.loc.ID] = l.loc
	}
	for i := range p.Samples {
		s := &p.Samples[i]
		for _, l := range s.rawLabels {
			if l[1] == 0 {
				continue // numeric label
			}
			if s.Labels == nil {
				s.Labels = map[string]string{}
			}
			s.Labels[str(l[0])] = str(l[1])
		}
		s.rawLabels = nil
		if len(s.Values) != len(p.SampleTypes) {
			return nil, fmt.Errorf("%w: sample has %d values for %d sample types", ErrNotProfile, len(s.Values), len(p.SampleTypes))
		}
	}
	return p, nil
}

func valueType(b []byte) ([2]int64, error) {
	var t [2]int64
	err := fields(b, func(n, wt int, v uint64, _ []byte) error {
		if n == 1 || n == 2 {
			t[n-1] = int64(v)
		}
		return nil
	})
	return t, err
}

func sample(b []byte) (Sample, error) {
	var s Sample
	err := fields(b, func(n, wt int, v uint64, b []byte) error {
		switch n {
		case 1:
			if wt == 2 {
				return packed(b, func(x uint64) { s.Locs = append(s.Locs, x) })
			}
			s.Locs = append(s.Locs, v)
		case 2:
			if wt == 2 {
				return packed(b, func(x uint64) { s.Values = append(s.Values, int64(x)) })
			}
			s.Values = append(s.Values, int64(v))
		case 3:
			var l [2]int64
			err := fields(b, func(n, wt int, v uint64, _ []byte) error {
				if n == 1 || n == 2 {
					l[n-1] = int64(v)
				}
				return nil
			})
			s.rawLabels = append(s.rawLabels, l)
			return err
		}
		return nil
	})
	return s, err
}

func fields(b []byte, f func(num, wireType int, v uint64, data []byte) error) error {
	return pbwire.Fields(b, f)
}

func packed(b []byte, f func(uint64)) error { return pbwire.Packed(b, f) }

// Combine adds profiles of the same kind, each multiplied by its sign (+1
// or -1): merging CPU profiles collected back to back, or subtracting a
// snapshot taken at the start of a capture from one taken at its end.
// Functions and locations are renumbered so that the inputs' tables do not
// collide; analysis aggregates by function name and file, so equal stacks
// of different inputs add up (and cancel when subtracted).
func Combine(parts []*Profile, signs []int64) (*Profile, error) {
	if len(parts) == 0 {
		return nil, errors.New("no profiles to combine")
	}
	out := &Profile{
		SampleTypes: parts[0].SampleTypes, DefaultType: parts[0].DefaultType,
		Locations: map[uint64]*Location{}, Functions: map[uint64]*Function{},
		PeriodType: parts[0].PeriodType, Period: parts[0].Period, TimeNanos: parts[0].TimeNanos,
	}
	var offset uint64
	for i, p := range parts {
		if len(p.SampleTypes) != len(out.SampleTypes) {
			return nil, errors.New("profiles of different kinds cannot be combined")
		}
		for j, t := range p.SampleTypes {
			if t != out.SampleTypes[j] {
				return nil, errors.New("profiles of different kinds cannot be combined")
			}
		}
		sign := int64(1)
		if i < len(signs) {
			sign = signs[i]
		}
		if sign > 0 {
			out.DurationNanos += p.DurationNanos
		}
		var maxID uint64
		fn := map[uint64]*Function{}
		for id, f := range p.Functions {
			c := *f
			c.ID = id + offset
			fn[id] = &c
			out.Functions[c.ID] = &c
			maxID = max(maxID, id)
		}
		for id, l := range p.Locations {
			c := &Location{ID: id + offset}
			for _, ln := range l.Lines {
				f := ln.Func
				if f != nil {
					f = fn[f.ID]
				}
				c.Lines = append(c.Lines, Line{Func: f, Line: ln.Line})
			}
			out.Locations[c.ID] = c
			maxID = max(maxID, id)
		}
		for _, s := range p.Samples {
			ns := Sample{Locs: make([]uint64, len(s.Locs)), Values: make([]int64, len(s.Values)), Labels: s.Labels}
			for k, l := range s.Locs {
				ns.Locs[k] = l + offset
			}
			for k, v := range s.Values {
				ns.Values[k] = v * sign
			}
			out.Samples = append(out.Samples, ns)
		}
		offset += maxID + 1
	}
	return out, nil
}

// Filter returns the profile restricted to the samples keep accepts.
func (p *Profile) Filter(keep func(Sample) bool) *Profile {
	c := *p
	c.Samples = nil
	for _, s := range p.Samples {
		if keep(s) {
			c.Samples = append(c.Samples, s)
		}
	}
	return &c
}
