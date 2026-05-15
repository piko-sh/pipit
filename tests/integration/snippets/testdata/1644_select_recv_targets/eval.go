package main

import (
	"fmt"
	"strings"
)

var global int
var gok bool

type S struct{ v int }

func run() string {
	var lines []string
	ch := make(chan int, 8)
	for i := 1; i <= 8; i++ {
		ch <- i
	}
	m := map[string]int{}
	s := &S{}
	arr := []int{0, 0}
	local := 0
	f := func() {
		select {
		case local = <-ch:
		}
	}
	f()
	select {
	case global = <-ch:
	}
	select {
	case m["k"], gok = <-ch:
	}
	select {
	case s.v = <-ch:
	}
	select {
	case *(&arr[1]) = <-ch:
	}
	var ok bool
	var v int
	select {
	case v, ok = <-ch:
	}
	select {
	case w, wok := <-ch:
		lines = append(lines, fmt.Sprint(w, wok, any(wok)))
	}
	select {
	case _, _ = <-ch:
	}
	closed := make(chan int)
	close(closed)
	var cv int = 99
	var cok bool = true
	select {
	case cv, cok = <-closed:
	}
	lines = append(lines, fmt.Sprint(local, global, m, gok, s.v, arr, v, ok, cv, cok))
	return strings.Join(lines, "\n")
}
