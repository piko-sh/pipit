package main

import "fmt"

type counter struct{ n int }

func (c *counter) inc()    { c.n++ }
func (c counter) get() int { return c.n }

type wrapped struct {
	counter
	label string
}

type pointerWrapped struct {
	*counter
}

type box[T any] struct{ items []T }

func (b *box[T]) add(v T) { b.items = append(b.items, v) }

type holder struct{ inner counter }

func call(f func()) { f() }

func run() string {
	var c counter
	inc := c.inc
	inc()
	call(c.inc)

	byValue := counter{n: 1}
	get := byValue.get
	byValue.n = 5

	p := &counter{n: 1}
	getThroughPointer := p.get
	p.n = 9

	var w wrapped
	incWrapped := w.inc
	incWrapped()

	pw := pointerWrapped{&counter{}}
	incPointerWrapped := pw.inc
	incPointerWrapped()

	var h holder
	incField := h.inner.inc
	incField()

	cs := make([]counter, 2)
	incElement := cs[1].inc
	incElement()

	var b box[string]
	add := b.add
	add("a")
	add("b")

	return fmt.Sprint(c.n, get(), getThroughPointer(), w.n, pw.n, h.inner.n, cs[1].n, b.items)
}
