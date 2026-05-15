package main

import (
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"
)

func countdown(n int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := n; i > 0; i-- {
			if !yield(i) {
				return
			}
		}
	}
}

func run() string {
	var lines []string
	next, stop := iter.Pull(slices.Values([]string{"a", "b", "c"}))
	var got []string
	for {
		v, ok := next()
		if !ok {
			break
		}
		got = append(got, v)
	}
	stop()
	lines = append(lines, fmt.Sprintf("%v %T", got, next))

	next2, stop2 := iter.Pull2(maps.All(map[string]int{"x": 1}))
	k, v, ok := next2()
	lines = append(lines, fmt.Sprint(k, v, ok))
	_, _, ok = next2()
	lines = append(lines, fmt.Sprint(ok))
	stop2()

	next3, stop3 := iter.Pull(countdown(100))
	first, _ := next3()
	second, _ := next3()
	stop3()
	after, ok3 := next3()
	lines = append(lines, fmt.Sprint(first, second, after, ok3))

	sum := 0
	for n := range countdown(4) {
		sum += n
	}
	lines = append(lines, fmt.Sprint(sum))
	return strings.Join(lines, "\n")
}
