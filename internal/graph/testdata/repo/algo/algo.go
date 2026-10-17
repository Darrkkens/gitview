package algo

func Fact(n int) int {
	if n <= 1 {
		return 1
	}
	return n * Fact(n-1)
}

func Even(n int) bool {
	if n == 0 {
		return true
	}
	return Odd(n - 1)
}

func Odd(n int) bool {
	if n == 0 {
		return false
	}
	return Even(n - 1)
}

func Double(x int) int { return 2 * x }

// Apply calls a function value it receives: the call is unresolved inside
// Apply, and Double is only a possible callee of Run.
func Apply(f func(int) int, x int) int { return f(x) }

func Run() int { return Apply(Double, 3) }
