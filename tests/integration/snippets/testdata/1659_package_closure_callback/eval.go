package main

import (
	"fmt"
	"strings"
)

type C struct {
	base  int
	depth int
}

func (c *C) bump() { c.depth++ }

func (c *C) val() int { return c.base }

var sink int

func noop() { sink++ }

func one() int { return 1 }

type holder struct {
	name string
	fn   func(c *C, x int) (int, bool)
}

var cases = []holder{
	{name: "noop-func", fn: func(c *C, x int) (int, bool) { noop(); return sink, true }},
	{name: "one-func", fn: func(c *C, x int) (int, bool) { return one(), true }},
	{name: "val-method", fn: func(c *C, x int) (int, bool) { return c.val(), true }},
	{name: "bump-method", fn: func(c *C, x int) (int, bool) { c.bump(); return c.depth, true }},
	{name: "mul-then-call", fn: func(c *C, x int) (int, bool) { c.base *= x; c.bump(); return c.base, true }},
	{name: "call-then-sub", fn: func(c *C, x int) (int, bool) { c.bump(); c.base -= x; return c.base, true }},
	{name: "method-expr", fn: (*C).tryA},
}

var viaInit []holder

func init() {
	viaInit = []holder{{name: "init", fn: func(c *C, x int) (int, bool) { c.base += x; c.bump(); return c.base, x > 0 }}}
}

func (c *C) tryA(x int) (int, bool) {
	c.base += x
	c.bump()
	return c.base, x > 5
}

func runCase(h holder, c *C) {
	v, ok := h.fn(c, 3)
	fmt.Fprintln(&out, h.name, v, ok, c.base, c.depth)
}

var out strings.Builder

func run() string {
	for _, h := range cases {
		runCase(h, &C{base: 1})
	}
	shared := &C{base: 10}
	runCase(viaInit[0], shared)
	runCase(viaInit[0], shared)
	fmt.Fprintln(&out, sink)
	return out.String()
}
