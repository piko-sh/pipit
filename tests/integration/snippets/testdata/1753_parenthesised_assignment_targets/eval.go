package main

import (
	"fmt"
	"strings"
	"sync"
)

type record struct {
	n     int
	pair  [2]int
	inner *record
}

type exclusive[T any] struct{ c chan *T }

func newExclusive[T any](initial *T) *exclusive[T] {
	e := &exclusive[T]{c: make(chan *T, 1)}
	e.c <- initial
	return e
}

func (e *exclusive[T]) acquire() *T  { return <-e.c }
func (e *exclusive[T]) release(p *T) { e.c <- p }

var total int

func run() string {
	var out strings.Builder

	x := 1
	(x)++
	(x)--
	(x) += 5
	(x) = (x) * 2
	fmt.Fprintln(&out, "local", x)

	p := &x
	(*p)++
	(*p) *= 2
	(*p)--
	(*(p))++
	fmt.Fprintln(&out, "pointer", x)

	r := &record{inner: &record{}}
	(r.n)++
	(r).n++
	(r.pair[1])++
	(r.pair[0]) += 3
	(r.inner.n) -= 4
	((*r).inner).n++
	fmt.Fprintln(&out, "fields", r.n, r.pair, r.inner.n)

	s := []int{1, 2}
	(s[0])++
	(s)[1]--
	(s[1]) <<= 3
	fmt.Fprintln(&out, "slice", s)

	m := map[string]int{}
	(m["a"])++
	(m["a"]) += 4
	(m)["b"]--
	fmt.Fprintln(&out, "map", m["a"], m["b"])

	(total)++
	(total) -= 3
	fmt.Fprintln(&out, "global", total)

	var small uint8 = 255
	(small)++
	(small) += 250
	fmt.Fprintln(&out, "narrow", small)

	step := func() {
		(x)++
		(*p) += 10
		(r.n) += 2
		(total)++
		(small)--
		(s[0]) = 7
	}
	step()
	step()
	fmt.Fprintln(&out, "closure", x, r.n, total, small, s)

	(x), (total) = (total), (x)
	fmt.Fprintln(&out, "swap", x, total)

	y := 1
	py := &(y)
	*py = 5
	bump := func() { y++ }
	bump()
	pn := &(r.n)
	*pn = 40
	pe := &(s[1])
	*pe = 90
	var key, value int
	for (key), (value) = range s {
	}
	fmt.Fprintln(&out, "address", y, r.n, s, key, value)

	held := record{}
	grid := [3]int{}
	alter := func() {
		(held).n = 5
		(held).pair[1]++
		(grid)[2] += 4
	}
	alter()
	pointed := record{}
	pp := &(pointed).n
	*pp = 7
	bumpPointed := func() { (pointed).n++ }
	bumpPointed()
	fmt.Fprintln(&out, "paths", held.n, held.pair, grid, pointed.n)

	val := 0
	ex := newExclusive(&val)
	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for range 10 {
			held := ex.acquire()
			(*held)++
			ex.release(held)
		}
	}
	wg.Add(2)
	go worker()
	go worker()
	wg.Wait()
	fmt.Fprintln(&out, "exclusive", val)

	return out.String()
}
