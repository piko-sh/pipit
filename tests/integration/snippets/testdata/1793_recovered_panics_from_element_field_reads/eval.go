package main

import (
	"fmt"
	"strings"
)

type cell struct {
	count int
	ratio float64
	name  string
	set   bool
	mask  uint32
	inner struct{ depth int }
}

func caught(body func()) (message string) {
	defer func() {
		if r := recover(); r != nil {
			message = fmt.Sprint(r)
		}
	}()
	body()
	return "no panic"
}

func count(cells []cell, i int) int { return cells[i].count }

func ratio(cells []cell, i int) float64 { return cells[i].ratio }

func name(cells []cell, i int) string { return cells[i].name }

func set(cells []cell, i int) bool { return cells[i].set }

func mask(cells []cell, i int) uint32 { return cells[i].mask }

func depth(cells []cell, i int) int { return cells[i].inner.depth }

func fromArray(cells *[2]cell, i int) int { return cells[i].count }

func letter(text string, i int) byte { return text[i] }

func letterNumber(text string, i int) int { return int(text[i]) }

func general(cells []cell, i int) cell { return cells[i] }

func named(cells []cell, i int) (value int, message string) {
	defer func() {
		if r := recover(); r != nil {
			message = fmt.Sprint(r)
		}
	}()
	return cells[i].count, "ok"
}

func total(cells []cell, n int) (sum float64, message string) {
	defer func() {
		if r := recover(); r != nil {
			message = fmt.Sprint(r)
		}
	}()
	for i := 0; i < n; i++ {
		sum += cells[i].ratio
		if cells[i].set {
			sum += float64(cells[i].mask)
		}
	}
	return sum, "ok"
}

func nested(cells []cell) string {
	outer := caught(func() {
		inner := caught(func() { count(cells, 9) })
		if !strings.Contains(inner, "index out of range") {
			panic("the inner panic was lost")
		}
		name(cells, -1)
	})
	return outer
}

func run() string {
	cells := []cell{{count: 1, ratio: 1.5, name: "a", set: true, mask: 7}, {count: 2, ratio: 2.5, name: "b"}}
	cells[1].inner.depth = 4
	var none []cell
	var noArray *[2]cell
	array := [2]cell{{count: 5}, {count: 6}}
	lines := []string{
		fmt.Sprint(count(cells, 1), ratio(cells, 1), name(cells, 1), set(cells, 0), mask(cells, 0), depth(cells, 1), fromArray(&array, 1)),
		caught(func() { count(cells, 2) }),
		caught(func() { ratio(cells, 3) }),
		caught(func() { name(cells, -1) }),
		caught(func() { set(cells, 4) }),
		caught(func() { mask(cells, 5) }),
		caught(func() { depth(cells, 6) }),
		caught(func() { count(none, 0) }),
		caught(func() { fromArray(&array, 2) }),
		caught(func() { fromArray(noArray, 0) }),
		nested(cells),
		caught(func() { letter("abc", -1) }),
		caught(func() { letter("abc", 3) }),
		caught(func() { letterNumber("abc", -2) }),
		caught(func() { letterNumber("abc", 7) }),
		caught(func() { general(cells, -1) }),
		caught(func() { general(cells, 2) }),
	}
	value, message := named(cells, 1)
	lines = append(lines, fmt.Sprint(value, " ", message))
	value, message = named(cells, 2)
	lines = append(lines, fmt.Sprint(value, " ", message))
	sum, message := total(cells, 2)
	lines = append(lines, fmt.Sprint(sum, " ", message))
	sum, message = total(cells, 3)
	lines = append(lines, fmt.Sprint(sum, " ", message), "still running")
	return strings.Join(lines, "\n")
}
