package main

import "fmt"

type shape interface{ area() int }

type square struct{ side int }

func (s square) area() int { return s.side * s.side }

type label string

func asShape(v any) (shape, bool) {
	s, ok := v.(shape)
	return s, ok
}

func isShape(v any) bool {
	switch v.(type) {
	case shape:
		return true
	default:
		return false
	}
}

func run() string {
	total := 0
	var kinds []bool
	for _, v := range []any{square{side: 2}, 3, label("x"), square{side: 3}} {
		if s, ok := asShape(v); ok {
			total += s.area()
		}
		kinds = append(kinds, isShape(v))
	}
	return fmt.Sprint(total, kinds)
}
