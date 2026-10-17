// Package layout computes deterministic graph positions. The same input
// always yields the same coordinates (no randomness, fixed iteration counts,
// index-ordered updates), so a node keeps its place across exports and
// across the views of the HTML report.
package layout

import "math"

// Point is a 2D position.
type Point struct{ X, Y float64 }

// Link is an attraction between two nodes.
type Link struct {
	A, B int
	W    float64
}

// GoldenAngle spreads points of a sunflower spiral evenly.
var GoldenAngle = math.Pi * (3 - math.Sqrt(5))

// Sunflower returns the i-th of n points evenly filling a disc of radius r.
func Sunflower(i, n int, r float64) Point {
	if n <= 1 {
		return Point{}
	}
	rad := r * math.Sqrt((float64(i)+0.5)/float64(n))
	a := float64(i) * GoldenAngle
	return Point{rad * math.Cos(a), rad * math.Sin(a)}
}

// Force runs a Fruchterman–Reingold layout in place. Repulsion is computed
// between every pair, so callers bound n·n·iters through the budget: the
// number of iterations is reduced for large n. gravity pulls nodes towards
// the origin; bound, when > 0, keeps nodes inside a disc of that radius.
// stop is polled between iterations.
func Force(pos []Point, links []Link, k float64, iters int, gravity, bound float64, stop func() bool) {
	n := len(pos)
	if n < 2 {
		return
	}
	const budget = 4e8
	if max := int(budget / float64(n*n)); iters > max {
		iters = max
	}
	if iters < 1 {
		iters = 1
	}
	disp := make([]Point, n)
	t := k * math.Sqrt(float64(n)) / 2
	cool := t / float64(iters+1)
	for it := 0; it < iters; it++ {
		if stop != nil && it%8 == 0 && stop() {
			return
		}
		for i := range disp {
			disp[i] = Point{}
		}
		for i := 0; i < n; i++ {
			pi := pos[i]
			for j := i + 1; j < n; j++ {
				dx, dy := pi.X-pos[j].X, pi.Y-pos[j].Y
				d2 := dx*dx + dy*dy
				if d2 < 1e-9 {
					// Coincident: separate along a fixed direction that
					// depends only on the indices.
					a := float64(i*31+j*17) * GoldenAngle
					dx, dy, d2 = math.Cos(a)*0.01, math.Sin(a)*0.01, 1e-4
				}
				f := k * k / d2
				disp[i].X += dx * f
				disp[i].Y += dy * f
				disp[j].X -= dx * f
				disp[j].Y -= dy * f
			}
		}
		for _, l := range links {
			if l.A == l.B {
				continue
			}
			dx, dy := pos[l.A].X-pos[l.B].X, pos[l.A].Y-pos[l.B].Y
			d := math.Sqrt(dx*dx + dy*dy)
			if d < 1e-9 {
				continue
			}
			f := d / k * l.W
			disp[l.A].X -= dx * f
			disp[l.A].Y -= dy * f
			disp[l.B].X += dx * f
			disp[l.B].Y += dy * f
		}
		for i := range pos {
			disp[i].X -= pos[i].X * gravity
			disp[i].Y -= pos[i].Y * gravity
			d := math.Sqrt(disp[i].X*disp[i].X + disp[i].Y*disp[i].Y)
			if d > 0 {
				s := math.Min(d, t) / d
				pos[i].X += disp[i].X * s
				pos[i].Y += disp[i].Y * s
			}
			if bound > 0 {
				if r := math.Hypot(pos[i].X, pos[i].Y); r > bound {
					pos[i].X *= bound / r
					pos[i].Y *= bound / r
				}
			}
		}
		t -= cool
	}
}

// Separate pushes discs (center, radius) apart until no two overlap by more
// than gap, keeping their relative arrangement. Pairs are processed in index
// order, so the result is deterministic.
func Separate(c []Point, r []float64, gap float64, iters int) {
	n := len(c)
	for it := 0; it < iters; it++ {
		moved := false
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				dx, dy := c[j].X-c[i].X, c[j].Y-c[i].Y
				d := math.Sqrt(dx*dx + dy*dy)
				need := r[i] + r[j] + gap
				if d >= need {
					continue
				}
				if d < 1e-9 {
					a := float64(i*31+j*17) * GoldenAngle
					dx, dy, d = math.Cos(a), math.Sin(a), 1
				}
				push := (need - d) / 2
				ux, uy := dx/d, dy/d
				// Bigger discs move less.
				wi := r[j] / (r[i] + r[j] + 1e-9)
				wj := 1 - wi
				c[i].X -= ux * push * 2 * wi
				c[i].Y -= uy * push * 2 * wi
				c[j].X += ux * push * 2 * wj
				c[j].Y += uy * push * 2 * wj
				moved = true
			}
		}
		if !moved {
			return
		}
	}
}

// Center translates points so their bounding box is centred on the origin.
func Center(p []Point) {
	if len(p) == 0 {
		return
	}
	minX, minY, maxX, maxY := p[0].X, p[0].Y, p[0].X, p[0].Y
	for _, q := range p {
		minX, maxX = math.Min(minX, q.X), math.Max(maxX, q.X)
		minY, maxY = math.Min(minY, q.Y), math.Max(maxY, q.Y)
	}
	cx, cy := (minX+maxX)/2, (minY+maxY)/2
	for i := range p {
		p[i].X -= cx
		p[i].Y -= cy
	}
}
