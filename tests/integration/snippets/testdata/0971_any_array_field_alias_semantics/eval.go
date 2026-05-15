package main

import "fmt"

type inner struct {
	tag   string
	count int
}

type carrier struct {
	notes [2]any
	pairs [2]inner
}

func run() string {
	c := &carrier{}
	c.pairs[0] = inner{tag: "a", count: 1}
	c.pairs[1] = inner{tag: "b", count: 2}
	note := inner{tag: "n", count: 9}
	c.notes[0] = 42
	c.notes[1] = note

	first := c.pairs[0]
	first.count += 10
	c.pairs[1] = first
	first.count += 100

	var boxed any = *c
	unboxed, _ := boxed.(carrier)
	tail := unboxed.pairs[1]

	return fmt.Sprintf("%v %v %d %d %d %s %d",
		c.notes[0], c.notes[1],
		c.pairs[0].count, c.pairs[1].count, first.count,
		tail.tag, tail.count)
}
