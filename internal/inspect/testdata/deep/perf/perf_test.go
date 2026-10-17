package perf

import "testing"

var sink any

func BenchmarkNewPoint(b *testing.B) {
	for i := 0; i < b.N; i++ {
		sink = NewPoint(i, i)
	}
}

func BenchmarkJoin(b *testing.B) {
	words := []string{"a", "bb", "ccc", "dddd"}
	for i := 0; i < b.N; i++ {
		sink = Join(words)
	}
}

func BenchmarkBusy(b *testing.B) {
	for i := 0; i < b.N; i++ {
		sink = Busy(20000)
	}
}

func TestSum(t *testing.T) {
	if Sum([]int{1, 2, 3}) != 6 {
		t.Fatal("sum")
	}
}
