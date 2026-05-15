package main

import (
	"fmt"
	"sort"
	"strings"
)

func makeInts() []int { return []int{4, 8, 16, 2} }

func identity(s []int) []int { return s }

func values(m map[int]int) []int {
	r := make([]int, 0, len(m))
	for _, v := range m {
		r = append(r, v)
	}
	return r
}

func generic[K comparable, V any](m map[K]V) []V {
	r := make([]V, 0, len(m))
	for _, v := range m {
		r = append(r, v)
	}
	return r
}

func run() string {
	var out []string

	a := makeInts()
	sort.Ints(a)
	out = append(out, fmt.Sprint("returned:", a))

	b := identity([]int{4, 8, 16, 2})
	sort.Ints(b)
	out = append(out, fmt.Sprint("through fn:", b))

	m := map[int]int{1: 2, 2: 4, 4: 8, 8: 16}
	c := values(m)
	sort.Ints(c)
	out = append(out, fmt.Sprint("from map:", c))

	d := generic(m)
	sort.Ints(d)
	out = append(out, fmt.Sprint("generic:", d))

	e := makeInts()
	n := copy(e, []int{1, 1, 1, 1})
	out = append(out, fmt.Sprint("copy into returned:", e, n))

	f := makeInts()
	sort.Sort(sort.Reverse(sort.IntSlice(f)))
	out = append(out, fmt.Sprint("reverse sort:", f))

	g := makeInts()
	sort.Slice(g, func(i, j int) bool { return g[i] < g[j] })
	out = append(out, fmt.Sprint("sort.Slice:", g))

	return strings.Join(out, "\n")
}
