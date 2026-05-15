package main

import (
	"fmt"
	"strings"
	"sync"
)

func run() string {
	var lines []string

	var g func() int
	var i int
	for i = range [3]int{} {
		if i == 0 {
			g = func() int { return i }
		}
	}
	lines = append(lines, fmt.Sprint(g()))

	w := 0
	tmp := 0
	f := func() int {
		tmp = w
		func() {
			func() { w++ }()
		}()
		return tmp
	}
	w = 5
	before := f()
	lines = append(lines, fmt.Sprint(before, w))
	func() {
		defer func() { w += 10 }()
	}()
	lines = append(lines, fmt.Sprint(w))

	var wg sync.WaitGroup
	ready := make(chan struct{})
	seen := 0
	total := 1
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ready
		seen = total
	}()
	total = 42
	close(ready)
	wg.Wait()
	lines = append(lines, fmt.Sprint(seen))

	var fs []func() int
	for j := 0; j < 3; j++ {
		fs = append(fs, func() int { return j })
	}
	var got []int
	for _, fn := range fs {
		got = append(got, fn())
	}
	lines = append(lines, fmt.Sprint(got))

	var hs []func() int
	for k := 0; k < 2; k++ {
		hs = append(hs, func() int { return k })
		k += 10
	}
	lines = append(lines, fmt.Sprint(len(hs), hs[0]()))

	q := 0
	var h func() int
	q, h = 1, func() int { return q }
	lines = append(lines, fmt.Sprint(h()))
	q = 7
	lines = append(lines, fmt.Sprint(h()))

	x, err := 1, error(nil)
	rd := func() (int, error) { return x, err }
	x, err = 2, fmt.Errorf("boom")
	y, err := 3, error(nil)
	v, e := rd()
	lines = append(lines, fmt.Sprint(v, e, y))

	s := []int{1}
	sum := func() int {
		t := 0
		for _, n := range s {
			t += n
		}
		return t
	}
	s = append(s, 2, 3)
	lines = append(lines, fmt.Sprint(sum()))

	name := "a"
	type pt struct{ X, Y int }
	p := pt{1, 2}
	show := func() string { return fmt.Sprint(name, p) }
	name = "b"
	p.X = 9
	p = pt{p.X, 8}
	lines = append(lines, show())

	return strings.Join(lines, "\n")
}
