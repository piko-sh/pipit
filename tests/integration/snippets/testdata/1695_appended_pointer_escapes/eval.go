package main

import "fmt"

type node struct {
	v int
}

func collect(base int) []*node {
	var out []*node
	for i := 0; i < 4; i++ {
		n := &node{v: base + i}
		out = append(out, n)
	}
	return out
}

func collectBoxed(base int) []any {
	var out []any
	for i := 0; i < 3; i++ {
		n := &node{v: base + i}
		out = append(out, n)
	}
	return out
}

func run() string {
	nodes := collect(1)
	extra := collect(100)
	boxed := collectBoxed(50)
	for _, n := range nodes {
		n.v *= 10
	}
	s := ""
	for _, n := range nodes {
		s += fmt.Sprint(n.v, ",")
	}
	for _, n := range extra {
		s += fmt.Sprint(n.v, ";")
	}
	for _, b := range boxed {
		s += fmt.Sprint(b.(*node).v, ".")
	}
	return s
}
