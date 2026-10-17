package perf

import (
	"sort"
	"strings"
)

type Point struct{ X, Y int }

// NewPoint returns a pointer, so the value escapes to the heap.
func NewPoint(x, y int) *Point {
	return &Point{X: x, Y: y}
}

// Sum keeps everything on the stack.
func Sum(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}

// Join allocates while building a string.
func Join(words []string) string {
	var b strings.Builder
	for _, w := range words {
		b.WriteString(w)
		b.WriteByte(' ')
	}
	return b.String()
}

// Busy burns CPU so a profile has samples in the module.
func Busy(n int) int {
	xs := make([]int, n)
	for i := range xs {
		xs[i] = (i * 7919) % 1000
	}
	sort.Ints(xs)
	acc := 0
	for i := 0; i < n; i++ {
		acc += xs[i] * xs[n-1-i]
	}
	return acc
}
