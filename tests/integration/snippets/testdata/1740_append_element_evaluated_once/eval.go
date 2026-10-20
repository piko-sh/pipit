package main

import (
	"errors"
	"fmt"
)

var calls int

func next() int {
	calls++
	return calls
}

func failure() error {
	calls++
	return errors.New("failure")
}

type shape interface{ Area() int }

type square int

func (s square) Area() int { return int(s) * int(s) }

func makeSquare() shape {
	calls++
	return square(calls)
}

func anys() int {
	var xs []any
	xs = append(xs, next())
	return len(xs)
}

func errs() int {
	var xs []error
	xs = append(xs, failure())
	return len(xs)
}

func shapes() int {
	var xs []shape
	xs = append(xs, makeSquare())
	return xs[0].Area()
}

func ints() int {
	var xs []int
	xs = append(xs, next())
	return xs[0]
}

func run() string {
	var out []string
	for _, step := range []struct {
		name string
		call func() int
	}{{"any", anys}, {"error", errs}, {"shape", shapes}, {"int", ints}} {
		calls = 0
		result := step.call()
		out = append(out, fmt.Sprint(step.name, " calls=", calls, " result=", result))
	}
	return fmt.Sprint(out)
}
