package main

import (
	"fmt"
	"strings"
)

type C struct {
	base  int
	depth int
}

type lowering struct {
	name string
	try  func(c *C, x int) (int, bool)
}

func (c *C) tryDefer(x int) (int, bool) {
	c.depth++
	defer func() { c.depth-- }()
	return c.base + c.depth + x, true
}

func (c *C) tryPointerCopy(x int) (int, bool) {
	p := c
	if p == nil {
		return -1, false
	}
	q := &c
	(*q).base += x
	return p.base, true
}

func (c *C) tryPassPointer(x int) (int, bool) {
	return read(c) + x, true
}

func read(c *C) int { return c.base }

var lowerings = []lowering{
	{name: "defer", try: (*C).tryDefer},
	{name: "pointer-copy", try: (*C).tryPointerCopy},
	{name: "pass-pointer", try: (*C).tryPassPointer},
}

var out strings.Builder

func run() string {
	c := &C{base: 1}
	for _, l := range lowerings {
		v, ok := l.try(c, 3)
		fmt.Fprintln(&out, l.name, v, ok, c.base, c.depth)
	}
	return out.String()
}
