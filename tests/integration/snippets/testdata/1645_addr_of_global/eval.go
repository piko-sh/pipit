package main

import (
	"fmt"
	"strings"
	"sync/atomic"
)

type Named int

type Counter struct{ n int }

func (c *Counter) Inc() { c.n++ }

var g int32
var u uint8 = 250
var s struct{ n int }
var p *int32
var q = &p
var counter int64
var arr [4]int
var sl []int = []int{1, 2, 3}
var slp = &sl
var m map[string]int
var mp = &m
var named Named = 3
var str string = "abc"
var strp = &str
var ctr Counter
var flt float64 = 1.5
var fp = &flt

func bump(p *int32) { *p += 10 }

func run() string {
	var lines []string
	atomic.AddInt32(&g, 5)
	bump(&g)
	lines = append(lines, fmt.Sprint(g, atomic.LoadInt32(&g)))
	pu := &u
	*pu += 10
	lines = append(lines, fmt.Sprint(u, *pu))
	lines = append(lines, fmt.Sprintf("%T %T %T %T %T", &g, &u, &s, &named, q))
	sp := &s
	sp.n = 7
	lines = append(lines, fmt.Sprint(s.n, (*sp).n))
	p = &g
	**q = 200
	lines = append(lines, fmt.Sprint(g, *p, **q, p == &g))
	atomic.AddInt64(&counter, 3)
	lines = append(lines, fmt.Sprint(counter))
	ap := &arr
	ap[2] = 9
	b := arr[:]
	b[0] = 1
	lines = append(lines, fmt.Sprint(arr, *ap, len(b)))
	*slp = append(*slp, 4)
	lines = append(lines, fmt.Sprint(sl, len(*slp), (*slp)[3]))
	*mp = map[string]int{"k": 1}
	lines = append(lines, fmt.Sprint(m["k"], len(*mp)))
	ctr.Inc()
	ctr.Inc()
	lines = append(lines, fmt.Sprint(ctr.n))
	*strp += "d"
	lines = append(lines, fmt.Sprint(str, *strp))
	np := &named
	*np++
	lines = append(lines, fmt.Sprint(named, *np))
	*fp *= 2
	lines = append(lines, fmt.Sprint(flt, *fp))
	f := func() { g++ }
	f()
	lines = append(lines, fmt.Sprint(g, *p))
	return strings.Join(lines, "\n")
}
