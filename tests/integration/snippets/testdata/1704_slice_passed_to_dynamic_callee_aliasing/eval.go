package main

import "fmt"

type holder struct{ xs []float64 }

func (h *holder) set(xs []float64) { h.xs = xs }

type setter interface{ set(xs []float64) }

type wrapper struct{ setter }

type callbacks struct{ keep func([]string) }

var kept []string

func apply[T any](fn func([]T), v []T) { fn(v) }

func run() string {
	var byClosure holder
	store := func(xs []float64) { byClosure.xs = xs }
	a := []float64{1}
	store(a)
	a[0] = 10

	var captured []float64
	assign := func(xs []float64) { captured = xs }
	b := []float64{2}
	assign(b)
	b[0] = 20

	var s setter = &holder{}
	c := []float64{3}
	s.set(c)
	c[0] = 30

	w := wrapper{&holder{}}
	d := []float64{4}
	w.set(d)
	d[0] = 40

	var byMethodValue holder
	set := byMethodValue.set
	e := []float64{5}
	set(e)
	e[0] = 50

	cb := callbacks{keep: func(ws []string) { kept = ws }}
	f := []string{"f"}
	cb.keep(f)
	f[0] = "F"

	var byGeneric holder
	g := []float64{7}
	apply(byGeneric.set, g)
	g[0] = 70

	return fmt.Sprint(byClosure.xs, captured, s.(*holder).xs, w.setter.(*holder).xs, byMethodValue.xs, kept, byGeneric.xs)
}
