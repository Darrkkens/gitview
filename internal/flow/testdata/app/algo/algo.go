package algo

// Fact is directly recursive.
func Fact(n int) int {
	if n <= 1 {
		return 1
	}
	return n * Fact(n-1)
}

// Ping and Pong form a cycle.
func Ping(n int) {
	if n > 0 {
		Pong(n - 1)
	}
}

func Pong(n int) { Ping(n) }

// Apply calls a function value: the target is not statically known.
func Apply(f func(int) int, x int) int { return f(x) }

type Notifier interface{ Notify() }

// Alert calls an interface with no implementation in the module.
func Alert(n Notifier) { n.Notify() }

type Shape interface{ Area() float64 }

type Square struct{ S float64 }
type Circle struct{ R float64 }

func (s Square) Area() float64  { return s.S * s.S }
func (c *Circle) Area() float64 { return 3 * c.R * c.R }

// Total dispatches through an interface with two implementations.
func Total(s Shape) float64 { return s.Area() }

// StartWorkers starts goroutines.
func StartWorkers() {
	go worker(1)
	go func() { L1() }()
}

func worker(id int) {}

// L1..L7 form a chain longer than the depth limit.
func L1() { L2() }
func L2() { L3() }
func L3() { L4() }
func L4() { L5() }
func L5() { L6() }
func L6() { L7() }
func L7() {}
