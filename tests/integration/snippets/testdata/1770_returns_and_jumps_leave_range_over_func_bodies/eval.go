package main

import (
	"errors"
	"fmt"
	"iter"
	"strings"
)

func count(n int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := range n {
			if !yield(i) {
				return
			}
		}
	}
}

func pairs(n int) iter.Seq2[int, string] {
	return func(yield func(int, string) bool) {
		for i := range n {
			if !yield(i, fmt.Sprint("s", i)) {
				return
			}
		}
	}
}

func named(n int) (result int) {
	for v := range count(100) {
		if v == n {
			return v
		}
	}
	return -1
}

func namedBare(n int) (result int, label string) {
	for v, s := range pairs(100) {
		if v == n {
			result, label = v, s
			return
		}
	}
	return -1, "none"
}

func deferred(n int) int {
	defer func() {}()
	for v := range count(100) {
		if v == n {
			return v
		}
	}
	return -1
}

func namedDeferred(n int) (result int) {
	defer func() { result *= 10 }()
	for v := range count(100) {
		if v == n {
			return v
		}
	}
	return -1
}

func twoResults(n int) (int, string) {
	defer func() {}()
	for v, s := range pairs(100) {
		if v == n {
			return v, s
		}
	}
	return -1, "missing"
}

func withError(n int) (int, error) {
	defer func() {}()
	for v := range count(100) {
		if v == n {
			return 0, errors.New(fmt.Sprint("hit ", v))
		}
	}
	return 1, nil
}

func nested(n int) (result int) {
	defer func() { result++ }()
	for a := range count(10) {
		for b := range count(10) {
			if a*10+b == n {
				return a*100 + b
			}
		}
	}
	return -1
}

func nestedPlain(n int) int {
	for a := range count(10) {
		for b := range count(10) {
			if a*10+b == n {
				return a*100 + b
			}
		}
	}
	return -1
}

func nestedTwo(n int) (int, string) {
	for a := range count(10) {
		for b, s := range pairs(10) {
			if a*10+b == n {
				return a, s
			}
		}
	}
	return -1, ""
}

func breakContinue() (out []int) {
	defer func() { out = append(out, 99) }()
outer:
	for a := range count(4) {
		for b := range count(4) {
			if b == 1 {
				continue
			}
			if b == 3 {
				continue outer
			}
			if a == 3 {
				break outer
			}
			out = append(out, a*10+b)
		}
	}
	return out
}

func rangeInsideRegularLoop(n int) (result string) {
	for i := 0; i < 3; i++ {
		for v := range count(5) {
			if i == 2 && v == n {
				return fmt.Sprint(i, v)
			}
		}
	}
	return "none"
}

func regularLoopInsideRange(n int) (result int) {
	defer func() { result = -result }()
	for v := range count(5) {
		for i := 0; i < 5; i++ {
			if i+v == n {
				return i*10 + v
			}
		}
	}
	return 0
}

var out strings.Builder

func show(values ...any) {
	fmt.Fprintln(&out, values...)
}

func run() string {
	fmt.Fprintln(&out, named(7), named(200))
	show(namedBare(3))
	fmt.Fprintln(&out, deferred(7), namedDeferred(7))
	show(twoResults(3))
	show(withError(4))
	show(withError(400))
	fmt.Fprintln(&out, nested(42), nested(999))
	fmt.Fprintln(&out, nestedPlain(42))
	show(nestedTwo(42))
	fmt.Fprintln(&out, breakContinue())
	fmt.Fprintln(&out, rangeInsideRegularLoop(3))
	fmt.Fprintln(&out, regularLoopInsideRange(6))
	return out.String()
}
