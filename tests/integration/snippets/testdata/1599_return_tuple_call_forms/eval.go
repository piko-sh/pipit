package main

import "fmt"

type pair struct{ n int }

func (p pair) two() (int, string) { return p.n, "m" }

func g() (int, string) { return 1, "a" }

func three() (int, string, bool) { return 7, "t", true }

func unnamed() (int, string) { return g() }

func named() (a int, b string) { return g() }

func deferred() (int, string) {
	defer func() {}()
	return g()
}

func namedDeferred() (a int, b string) {
	defer func() { a++ }()
	return g()
}

func viaMethod() (int, string) {
	defer func() {}()
	p := pair{n: 5}
	return p.two()
}

func viaClosure() (int, string, bool) {
	f := three
	defer func() {}()
	return f()
}

func run() string {
	x1, y1 := unnamed()
	x2, y2 := named()
	x3, y3 := deferred()
	x4, y4 := namedDeferred()
	x5, y5 := viaMethod()
	x6, y6, z6 := viaClosure()
	return fmt.Sprint(x1, y1, " ", x2, y2, " ", x3, y3, " ", x4, y4, " ", x5, y5, " ", x6, y6, z6)
}
