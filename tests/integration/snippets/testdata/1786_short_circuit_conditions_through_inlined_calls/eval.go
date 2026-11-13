package main

import (
	"fmt"
	"strings"
)

type view struct {
	data   []byte
	offset int
}

func (v view) at(i int) byte {
	index := v.offset + i
	if index >= len(v.data) && index < len(v.data)+4 {
		return 0
	}
	return v.data[index]
}

func between(x, low, high int) bool {
	return x >= low && x < high
}

func outside(x, low, high int) bool {
	return x < low || x >= high
}

func classify(x int) string {
	if x > 3 && x < 7 || x == 10 {
		return "mid"
	}
	if !(x >= 0) || x > 100 && x != 200 {
		return "far"
	}
	return "near"
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

var evaluations []string

func noted(name string, value bool) bool {
	evaluations = append(evaluations, name)
	return value
}

func sum(v view, n int) int {
	total := 0
	for i := 0; i < n; i++ {
		total = total*3 + int(v.at(i))
	}
	return total
}

func counts(limit int) (inside, out, both int) {
	for i := -2; i < limit; i++ {
		if between(i, 0, 5) {
			inside++
		}
		if outside(i, 1, 4) {
			out++
		}
		if between(i, 0, 5) && outside(i, 1, 4) {
			both++
		}
	}
	return inside, out, both
}

func run() string {
	v := view{data: []byte{5, 6, 7, 8}, offset: 1}
	inside, out, both := counts(9)
	lines := []string{
		fmt.Sprint("at ", v.at(0), v.at(2), v.at(3), v.at(6)),
		"past the tail " + caught(func() { _ = v.at(7) }),
		"before the head " + caught(func() { _ = view{data: v.data, offset: -3}.at(1) }),
		fmt.Sprint("sum ", sum(v, 3), sum(v, 7), sum(view{}, 4)),
		fmt.Sprint("counts ", inside, out, both),
	}
	names := []string{}
	for _, x := range []int{-1, 0, 5, 10, 150, 200} {
		names = append(names, classify(x))
	}
	lines = append(lines, "classify "+strings.Join(names, ","))

	evaluations = nil
	first := noted("a", false) && noted("b", true)
	second := noted("c", true) || noted("d", false)
	third := noted("e", true) && noted("f", false) || noted("g", true)
	lines = append(lines, fmt.Sprint("order ", first, second, third, " ", strings.Join(evaluations, "")))
	return strings.Join(lines, "\n")
}
