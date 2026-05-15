package main

import (
	"fmt"
	"strings"
)

var trace []string

func idx(n int) int   { trace = append(trace, fmt.Sprint("idx", n)); return n }
func val(n int) int   { trace = append(trace, fmt.Sprint("val", n)); return n }
func arr() []int      { trace = append(trace, "arr"); return make([]int, 4) }
func two() (int, int) { trace = append(trace, "two"); return 1, 2 }

type S struct{ a, b int }

func sel() *S { trace = append(trace, "sel"); return &S{} }

func take() string {
	out := strings.Join(trace, ",")
	trace = nil
	return out
}

func run() string {
	var lines []string
	xs := make([]int, 4)
	xs[idx(1)] = val(10)
	lines = append(lines, take()+" "+fmt.Sprint(xs))
	arr()[idx(2)] = val(20)
	lines = append(lines, take())
	sel().a = val(30)
	lines = append(lines, take())
	p := &xs[0]
	*p = val(40)
	lines = append(lines, take()+" "+fmt.Sprint(xs[0]))

	i := 0
	ys := []int{1, 2, 3}
	i, ys[i] = 2, 9
	lines = append(lines, fmt.Sprint(i, ys))
	a, b := 1, 2
	a, b = b, a
	lines = append(lines, fmt.Sprint(a, b))
	m := map[string]int{}
	k := "x"
	k, m[k] = "y", 5
	lines = append(lines, fmt.Sprint(k, m))
	zs := make([]int, 3)
	zs[idx(0)], zs[idx(1)] = two()
	lines = append(lines, take()+" "+fmt.Sprint(zs))
	j := 0
	ws := []int{5, 6, 7}
	j, ws[j] = two()
	lines = append(lines, fmt.Sprint(j, ws, take()))

	q := &S{}
	q.a, q.b = val(1), val(2)
	cnt := 0
	next := func() *S { cnt++; return q }
	next().a += 5
	*(&next().b) += 7
	ptr := func() *int { cnt++; return &xs[3] }
	*ptr() += 3
	xs[idx(2)] += val(100)
	lines = append(lines, fmt.Sprint(q, cnt, xs, take()))

	ch := make(chan int, 2)
	ch <- 4
	ch <- 5
	rs := make([]int, 2)
	rs[<-ch-4] = <-ch
	lines = append(lines, fmt.Sprint(rs))
	return strings.Join(lines, "\n")
}
