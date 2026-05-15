package main

import "fmt"

type point struct{ X, Y int }

var trace string

func note(s string) int { trace += s; return 0 }

var _ = note("first")
var second = note("second")

func captured() (x int) {
	defer func() func() {
		return func() { trace += fmt.Sprint(" x=", x) }
	}()()
	return 42
}

func kindOf[T any](v any) string {
	switch v.(type) {
	case T:
		return "T"
	case int:
		return "int"
	default:
		return "other"
	}
}

func run() string {
	captured()
	return fmt.Sprint(trace, " ", second, " ", kindOf[float64](6.0), kindOf[float64](7), kindOf[int](8), kindOf[string](true), " ", point{1, 2}, fmt.Sprintf("%+v", point{3, 4}))
}
