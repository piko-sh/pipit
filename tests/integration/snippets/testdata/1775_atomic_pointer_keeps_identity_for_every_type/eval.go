package main

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

type config struct {
	name    string
	retries int
}

type label string

var out strings.Builder

func run() string {
	var cp atomic.Pointer[config]
	fmt.Fprintln(&out, cp.Load() == nil)
	first := &config{name: "a", retries: 1}
	cp.Store(first)
	loaded := cp.Load()
	fmt.Fprintln(&out, loaded == first, loaded.name, loaded.retries)
	loaded.retries = 5
	fmt.Fprintln(&out, first.retries)
	second := &config{name: "b"}
	old := cp.Swap(second)
	fmt.Fprintln(&out, old == first, cp.Load() == second)
	fmt.Fprintln(&out, cp.CompareAndSwap(first, &config{}), cp.CompareAndSwap(second, first), cp.Load().name)

	var sp atomic.Pointer[string]
	s := "hello"
	sp.Store(&s)
	fmt.Fprintln(&out, *sp.Load(), sp.Load() == &s)
	*sp.Load() = "changed"
	fmt.Fprintln(&out, s)

	var lp atomic.Pointer[label]
	l := label("tag")
	lp.Store(&l)
	fmt.Fprintf(&out, "%v %T\n", *lp.Load(), lp.Load())

	var slp atomic.Pointer[[]int]
	xs := []int{1, 2}
	slp.Store(&xs)
	*slp.Load() = append(*slp.Load(), 3)
	fmt.Fprintln(&out, xs)

	var counter atomic.Pointer[int]
	zero := 0
	counter.Store(&zero)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				for {
					current := counter.Load()
					next := *current + 1
					if counter.CompareAndSwap(current, &next) {
						break
					}
				}
			}
		})
	}
	wg.Wait()
	fmt.Fprintln(&out, *counter.Load())
	return out.String()
}
