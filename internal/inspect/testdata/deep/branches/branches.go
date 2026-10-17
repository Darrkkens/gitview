package branches

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

var (
	ErrInvalidID = errors.New("invalid id")
	ErrNotFound  = errors.New("not found")
)

func exists(id int64) bool { return id%2 == 0 }

// GetClient has two early returns and a final success return.
func GetClient(id int64) error {
	if id <= 0 {
		return ErrInvalidID
	}

	if !exists(id) {
		return ErrNotFound
	}

	return nil
}

// IfElse has an else branch and a merge point.
func IfElse(x int) string {
	var s string
	if x > 10 {
		s = "big"
	} else {
		s = "small"
	}
	return s
}

// Grade uses an expression switch with a default and a multi-value case.
func Grade(n int) string {
	switch n {
	case 10, 9:
		return "A"
	case 8:
		return "B"
	default:
		return "C"
	}
}

// Kind uses a type switch without default.
func Kind(v any) string {
	switch v.(type) {
	case int, int64:
		return "integer"
	case string:
		return "text"
	}
	return "other"
}

// Sum loops with continue and break.
func Sum(xs []int) int {
	total := 0
	for i := 0; i < len(xs); i++ {
		if xs[i] < 0 {
			continue
		}
		if xs[i] > 100 {
			break
		}
		total += xs[i]
	}
	return total
}

// Each ranges over a map.
func Each(m map[string]int) (n int) {
	for _, v := range m {
		n += v
	}
	return
}

// Wait selects between two channels without default.
func Wait(a, b chan int) int {
	select {
	case v := <-a:
		return v
	case <-b:
		return -1
	}
}

// Poll selects with a default case.
func Poll(a chan int) (int, bool) {
	select {
	case v := <-a:
		return v, true
	default:
		return 0, false
	}
}

// Parse returns errors early and defers a cleanup.
func Parse(s string) (n int, err error) {
	defer fmt.Println("parsed", s)
	n, err = strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("parse %q: %w", s, err)
	}
	if n < 0 {
		panic("negative")
	}
	return n, nil
}

// Exit never returns on the error path.
func Exit(code int) {
	if code != 0 {
		os.Exit(code)
	}
	fmt.Println("ok")
}

// Unreachable has statements after a return.
func Unreachable() int {
	return 1
	fmt.Println("never")
	return 2
}

// Forever loops until a labelled break.
func Forever(ch chan int) int {
	count := 0
loop:
	for {
		v, ok := <-ch
		if !ok {
			break loop
		}
		count += v
	}
	return count
}

// NestedSwitch has a switch inside the default of a type switch.
func NestedSwitch(v any, y int) string {
	switch v.(type) {
	case string:
		return "s"
	default:
		switch y {
		case 1:
			return "one"
		}
	}
	return "other"
}

// NestedSelect has a select inside the default of a select.
func NestedSelect(a, b chan int) int {
	select {
	case v := <-a:
		return v
	default:
		select {
		case v := <-b:
			return v
		}
	}
}

// CompoundErr tests an error with a compound condition.
func CompoundErr(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return 0, err
	}
	return n, nil
}
