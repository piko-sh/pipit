package main

import "fmt"

type Inner struct {
	P *int
	N int
}

func (i *Inner) Bump() int { i.N += 100; return i.N }

type Middle struct {
	*Inner
}

type Outer struct {
	Middle
}

func run() string {
	a, b, c := 1, 2, 3
	o := Outer{Middle: Middle{Inner: &Inner{P: &a}}}

	o.P = &b
	write := *o.P

	o.N = 5
	o.N += 7
	o.N--

	bumped := o.Bump()

	p := &Inner{P: &c, N: 40}
	p.P = &a
	p.N += 2

	return fmt.Sprintf("write=%d n=%d bumped=%d pP=%d pN=%d", write, o.N, bumped, *p.P, p.N)
}
