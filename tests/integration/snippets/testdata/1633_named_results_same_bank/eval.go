package main

import "fmt"

func pair(ce uint64) (a uint64, b uint64) { return ce + 1, ce + 2 }

func ints(x int) (a int, b int) { return x + 1, x + 2 }

func mixed(x int, s string) (n int, t string, ok bool) {
	n = x * 2
	t = s + "!"
	ok = n > 0
	return
}

func split(ce uint64) (index uint32, offset uint64) {
	h := uint32(ce)
	return h >> 5, ce + 1
}

func withDefer(x int) (a int, b int) {
	defer func() { a++ }()
	return x, x * 10
}

func run() string {
	a, b := pair(10)
	c, d := ints(10)
	n, t, ok := mixed(21, "go")
	index, offset := split(0x123400005678)
	e, f := withDefer(5)
	g, h := pair(1)
	i, j := ints(1)
	return fmt.Sprintf("%d %d|%d %d|%d %s %v|%d %d|%d %d|%d %d|%d %d", a, b, c, d, n, t, ok, index, offset, e, f, g, h, i, j)
}
