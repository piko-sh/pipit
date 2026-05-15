package main

import "fmt"

type List *struct {
	v    int
	next List
}

type node struct {
	label string
	next  *node
}

func run() string {
	tail := List(&struct {
		v    int
		next List
	}{v: 2})
	head := List(&struct {
		v    int
		next List
	}{v: 1, next: tail})

	second := &node{label: "b"}
	first := &node{label: "a", next: second}

	return fmt.Sprintf("%d %v %d | %s %s %v",
		head.v, head.next != nil, head.next.v, first.label, first.next.label, first.next.next == nil)
}
