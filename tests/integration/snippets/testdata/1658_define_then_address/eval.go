package main

import (
	"fmt"
	"strings"
)

func two() (int, error) { return 1, nil }

func plain(cond bool) int {
	x := 1
	if cond {
		p := &x
		*p = 5
	}
	return x
}

func multi(cond bool) int {
	x, err := two()
	if err != nil {
		return -1
	}
	if cond {
		p := &x
		*p = 5
	}
	return x
}

func declared(cond bool) int {
	var x int
	x = 1
	if cond {
		p := &x
		*p = 5
	}
	return x
}

func assigned(cond bool) int {
	x := 0
	x, _ = two()
	if cond {
		p := &x
		*p = 5
	}
	return x
}

func bump(p *int) { *p += 10 }

func multiCall(cond bool) int {
	x, err := two()
	if err != nil {
		return -1
	}
	if cond {
		bump(&x)
	}
	return x
}

var out strings.Builder

func run() string {
	fmt.Fprintln(&out, mapOk(false), mapOk(true), assertOk(false), assertOk(true), recvOk(false), recvOk(true))
	fmt.Fprintln(&out, plain(false), plain(true), multi(false), multi(true), declared(false), declared(true), assigned(false), assigned(true), multiCall(false), multiCall(true))
	return out.String()
}

func mapOk(cond bool) int {
	m := map[string]int{"a": 1}
	x, ok := m["a"]
	if !ok {
		return -1
	}
	if cond {
		bump(&x)
	}
	return x
}

func assertOk(cond bool) int {
	var v any = 1
	x, ok := v.(int)
	if !ok {
		return -1
	}
	if cond {
		bump(&x)
	}
	return x
}

func recvOk(cond bool) int {
	ch := make(chan int, 1)
	ch <- 1
	x, ok := <-ch
	if !ok {
		return -1
	}
	if cond {
		bump(&x)
	}
	return x
}
