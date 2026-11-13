package main

import (
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"
)

type Names []string

func (n Names) String() string { return "Names" }

type Index map[string]int

func (Index) String() string { return "Index" }

type Pair [2]int

func (Pair) String() string { return "Pair" }

type Fn func() int

func (Fn) String() string { return "Fn" }

func seq(n int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := range n {
			if !yield(i) {
				return
			}
		}
	}
}

func Gen[T any](xs ...T) iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, x := range xs {
			if !yield(x) {
				return
			}
		}
	}
}

func firstNames() Names {
	for i := range seq(3) {
		if i == 1 {
			return []string{"a", "b"}
		}
	}
	return nil
}

func firstIndex() (Index, error) {
	for range seq(2) {
		return map[string]int{"x": 1}, nil
	}
	return nil, nil
}

func pair() Pair {
	for x := range Gen(1, 2, 3) {
		if x == 2 {
			return [2]int{x, x}
		}
	}
	return [2]int{}
}

func fn() Fn {
	for range seq(1) {
		return func() int { return 7 }
	}
	return nil
}

func nested() Names {
	for i := range seq(2) {
		for j := range Gen("p", "q") {
			if i == 1 && j == "q" {
				return []string{j}
			}
		}
	}
	return nil
}

func named2() (n Names) {
	for k := range maps.Keys(map[string]int{"z": 1}) {
		return []string{k}
	}
	return
}

func viaSlices() Names {
	for _, v := range slices.All([]string{"s"}) {
		return []string{v}
	}
	return nil
}

func gen[T any](x T) Names {
	for range Gen(x) {
		return []string{fmt.Sprint(x)}
	}
	return nil
}

func show(v any) string {
	_, isNames := v.(Names)
	_, isPlain := v.([]string)
	return fmt.Sprintf("%T %v %v %v", v, v, isNames, isPlain)
}

var out strings.Builder

func run() string {
	fmt.Fprintln(&out, show(firstNames()))
	ix, _ := firstIndex()
	fmt.Fprintln(&out, show(ix))
	fmt.Fprintln(&out, show(pair()))
	f := fn()
	fmt.Fprintln(&out, fmt.Sprintf("%T %v", f, f), f())
	fmt.Fprintln(&out, show(nested()))
	fmt.Fprintln(&out, show(named2()))
	fmt.Fprintln(&out, show(viaSlices()))
	fmt.Fprintln(&out, show(gen(5)))
	var s fmt.Stringer = firstNames()
	fmt.Fprintln(&out, s.String())
	return out.String()
}
