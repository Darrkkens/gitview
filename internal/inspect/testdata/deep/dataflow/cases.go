package dataflow

import (
	"strconv"
	"strings"

	"example.com/deep/model"
)

// Locals moves a value through local variables and a conversion.
func Locals(raw string) int {
	trimmed := strings.TrimSpace(raw)
	n, _ := strconv.Atoi(trimmed)
	doubled := n * 2
	return doubled
}

// Merge assigns the same variable on two branches.
func Merge(flag bool, a, b string) string {
	v := a
	if flag {
		v = b
	}
	return v
}

// Wrap passes its parameter to another function and returns the result.
func Wrap(s string) string {
	return suffix(s)
}

func suffix(x string) string { return x + "!" }

// Caller calls Wrap with a literal and a parameter.
func Caller(p string) (string, string) {
	return Wrap("lit"), Wrap(p)
}

// Fields stores a value in a struct field and reads it back.
func Fields(name string) string {
	c := model.Client{}
	c.Name = name
	return c.Name
}

// Pointer writes through a pointer.
func Pointer(v int) int {
	x := 0
	p := &x
	*p = v
	return x
}

// SetName writes a parameter through a pointer parameter.
func SetName(c *model.Client, name string) {
	c.Name = name
}

type Store interface{ Put(key string) }

type memStore struct{ keys []string }

func (m *memStore) Put(key string) { m.keys = append(m.keys, key) }

// Iface passes a value through an interface with one implementation.
func Iface(s Store, key string) {
	s.Put(key)
}

// Dynamic calls a function value: the target is unknown statically.
func Dynamic(f func(string) string, in string) string {
	return f(in)
}

// Closure captures a variable.
func Closure(in string) func() string {
	return func() string { return in + "?" }
}

// Shadow declares two variables named v.
func Shadow(a int) int {
	v := a + 1
	{
		v := v * 3
		_ = v
	}
	return v
}

// idf returns its argument through another call: a trace entering from
// ContextA must come back to ContextA only.
func idf(s string) string { return wrapf(s) }

func wrapf(s string) string { return s }

func ContextA() string {
	x := idf("a")
	return x
}

func ContextB() { idf("b") }

type acc struct{ n int }

// LoopCarried reads a.n before the write later in the loop body.
func LoopCarried(xs []int) int {
	var a acc
	a.n = 1
	total := 0
	for _, x := range xs {
		total += a.n
		a.n = x
	}
	return total
}

type Greeter struct{ name string }

func (g *Greeter) Hello(p string) string { return g.name + p }

// Bound calls a method value: the receiver is bound in the closure.
func Bound(x *Greeter) string {
	f := x.Hello
	return f("!")
}

// Outer returns a closure that uses its parameter.
func Outer(in string) func() string {
	return func() string { return strings.ToUpper(in) }
}
