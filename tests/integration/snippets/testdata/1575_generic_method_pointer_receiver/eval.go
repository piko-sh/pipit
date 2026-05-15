package main

import "fmt"

type T struct{}

func (t *T) m[P any]() string {
	var x P
	return fmt.Sprintf("%T", x)
}

type G[P any] struct{}

func (g *G[P]) m[Q any]() string {
	var p P
	var q Q
	return fmt.Sprintf("%T %T", p, q)
}

func run() string {
	return (&T{}).m[int]() + " / " + (&G[int]{}).m[bool]()
}
