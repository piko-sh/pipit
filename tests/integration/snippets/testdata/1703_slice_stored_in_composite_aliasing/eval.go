package main

import "fmt"

type floats struct{ xs []float64 }

type words struct{ ws []string }

func wrap(ws []string) words { return words{ws} }

func wrapVia(ws []string) words { return wrap(ws) }

func (w *words) set(ws []string) { w.ws = ws }

type labels []string

var kept labels

func (l labels) keep() { kept = l }

func run() string {
	a := []float64{1, 2}
	literal := floats{xs: a}
	a[0] = 10

	b := make([]float64, 2)
	array := [2][]float64{b, b}
	b[1] = 2.5

	c := []string{"p", "q"}
	var field words
	field.ws = c
	c[0] = "r"

	d := []bool{false}
	byKey := map[string][]bool{"d": d}
	d[0] = true

	e := []byte("ab")
	nested := [][]byte{}
	nested = append(nested, e)
	e[1] = 'z'

	f := []uint{3}
	ch := make(chan []uint, 1)
	ch <- f
	f[0] = 4

	g := []string{"g"}
	direct := wrap(g)
	g[0] = "G"

	h := []string{"h"}
	transitive := wrapVia(h)
	h[0] = "H"

	i := []string{"i"}
	var method words
	method.set(i)
	i[0] = "I"

	j := labels{"j"}
	j.keep()
	j[0] = "J"

	k := []string{"k"}
	var expression words
	(*words).set(&expression, k)
	k[0] = "K"

	l := []float64{1, 2}
	alias := l[1:]
	viaAlias := floats{alias}
	l[1] = 20

	return fmt.Sprint(literal.xs, array, field.ws, byKey["d"], string(nested[0]), <-ch, direct.ws, transitive.ws,
		method.ws, kept, expression.ws, viaAlias.xs)
}
