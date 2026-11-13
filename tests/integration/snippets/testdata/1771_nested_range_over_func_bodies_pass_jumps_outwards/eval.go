package main

import (
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
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

func pair(a, b int) (int, error) {
	if b == 0 {
		return 0, errors.New("zero")
	}
	return a / b, nil
}

func tupleReturn(n int) (int, error) {
	defer func() {}()
	for v := range count(10) {
		if v == n {
			return pair(10, v)
		}
	}
	return -1, nil
}

func typedNil(n int) (*int, error) {
	for v := range count(10) {
		if v == n {
			return nil, nil
		}
	}
	x := 1
	return &x, errors.New("none")
}

func threeLevels() (out []string) {
loop:
	for i := 0; i < 3; i++ {
		for a := range count(3) {
			for b := range count(3) {
				if b == 1 {
					continue loop
				}
				if i == 2 {
					break loop
				}
				out = append(out, fmt.Sprint(i, a, b))
			}
		}
	}
	return out
}

func nestedGoto(n int) (result string) {
	for a := range count(5) {
		for b := range count(5) {
			if a*5+b == n {
				goto done
			}
		}
	}
	return "missed"
done:
	return "hit"
}

func backwardGoto() (tries int) {
again:
	tries++
	for v := range count(5) {
		if v == 2 && tries < 3 {
			goto again
		}
	}
	return tries
}

func nestedBare(n int) (result int, note string) {
	defer func() { note += "!" }()
	for a := range count(5) {
		for b := range count(5) {
			result = a*10 + b
			note = "at"
			if result == n {
				return
			}
		}
	}
	return -1, "none"
}

func nativeIterator(m map[string]int) (string, int) {
	for k, v := range maps.All(m) {
		if v == 2 {
			return k, v
		}
	}
	return "", 0
}

func nativeSlices(s []string) (index int) {
	defer func() { index += 1000 }()
	for i, v := range slices.All(s) {
		if v == "c" {
			return i
		}
	}
	return -1
}

var out strings.Builder

func show(values ...any) {
	fmt.Fprintln(&out, values...)
}

func run() string {
	show(tupleReturn(5))
	show(tupleReturn(0))
	p, err := typedNil(3)
	fmt.Fprintln(&out, p == nil, err == nil)
	fmt.Fprintln(&out, threeLevels())
	fmt.Fprintln(&out, nestedGoto(7), nestedGoto(70))
	fmt.Fprintln(&out, backwardGoto())
	show(nestedBare(23))
	show(nativeIterator(map[string]int{"a": 1, "b": 2}))
	fmt.Fprintln(&out, nativeSlices([]string{"a", "b", "c"}))
	return out.String()
}
