package main

import (
	"fmt"
	"strings"
	"sync"
)

type point struct{ x, y int }

func continued() []int {
	var ps []*int
	for i := 0; i < 6; i++ {
		if i%2 == 0 {
			ps = append(ps, &i)
			continue
		}
		ps = append(ps, &i)
	}
	return deref(ps)
}

func writeThrough() []int {
	var ps []*int
	for i := 0; i < 10; i++ {
		p := &i
		*p += 2
		ps = append(ps, p)
	}
	return deref(ps)
}

func laterWrite() []int {
	var ps []*int
	for i := 0; i < 3; i++ {
		ps = append(ps, &i)
	}
	*ps[0] = 100
	return deref(ps)
}

func twoVariables() string {
	var ps []*int
	var qs []*string
	for i, s := 0, "a"; i < 3; i, s = i+1, s+"b" {
		ps = append(ps, &i)
		qs = append(qs, &s)
	}
	out := ""
	for k := range ps {
		out += fmt.Sprint(*ps[k], *qs[k], " ")
	}
	return out
}

func typed() string {
	var fs []*float64
	var bs []*bool
	b := false
	for f := 0.5; f < 2; f += 0.5 {
		fs = append(fs, &f)
	}
	for v := true; !b; v = !v {
		bs = append(bs, &v)
		b = len(bs) == 2
	}
	return fmt.Sprint(*fs[0], *fs[1], *fs[2], *bs[0], *bs[1])
}

func structsAndArrays() string {
	var ps []*point
	for p := (point{}); p.x < 3; p.x, p.y = p.x+1, p.y+10 {
		ps = append(ps, &p)
	}
	var as []*[2]int
	for a := [2]int{}; a[0] < 2; a[0]++ {
		as = append(as, &a)
	}
	out := ""
	for _, p := range ps {
		out += fmt.Sprint(*p, " ")
	}
	for _, a := range as {
		out += fmt.Sprint(*a, " ")
	}
	return out
}

func closureAndAddress() []int {
	var fs []func() int
	var ps []*int
	for i := 0; i < 3; i++ {
		fs = append(fs, func() int { return i * 10 })
		ps = append(ps, &i)
	}
	out := deref(ps)
	for _, f := range fs {
		out = append(out, f())
	}
	return out
}

func closureWrites() []int {
	var fs []func()
	var ps []*int
	for i := 0; i < 3; i++ {
		ps = append(ps, &i)
		fs = append(fs, func() { i += 100 })
	}
	for _, f := range fs {
		f()
	}
	return deref(ps)
}

func nested() []int {
	var ps []*int
outer:
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if j == 1 {
				continue outer
			}
			ps = append(ps, &i, &j)
		}
	}
	return deref(ps)
}

func goroutines() []int {
	results := make([]int, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = i * i
		}()
	}
	wg.Wait()
	return results
}

func noInit() []int {
	var ps []*int
	i := 0
	for ; i < 3; i++ {
		ps = append(ps, &i)
	}
	return deref(ps)
}

func deref(ps []*int) []int {
	out := []int{}
	for _, p := range ps {
		out = append(out, *p)
	}
	return out
}

var out strings.Builder

func run() string {
	fmt.Fprintln(&out, continued())
	fmt.Fprintln(&out, writeThrough())
	fmt.Fprintln(&out, laterWrite())
	fmt.Fprintln(&out, twoVariables())
	fmt.Fprintln(&out, typed())
	fmt.Fprintln(&out, structsAndArrays())
	fmt.Fprintln(&out, closureAndAddress())
	fmt.Fprintln(&out, closureWrites())
	fmt.Fprintln(&out, nested())
	fmt.Fprintln(&out, goroutines())
	fmt.Fprintln(&out, noInit())
	return out.String()
}
