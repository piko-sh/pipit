package main

import (
	"fmt"
	"strings"
)

type Base struct{ name string }

func (b Base) Name() string { return b.name }

type Dog struct {
	Base
	sound string
}

func (d Dog) Sound() string    { return d.sound }
func (d *Dog) Rename(n string) { d.name = n }

type Namer interface{ Name() string }

type T int

func (t T) M() int { return int(t) * 2 }

type B struct{ T }

type Doubler interface{ M() int }

type Inner struct{ v int }

func (i *Inner) V() int { return i.v }

type Outer struct{ *Inner }

type Plain struct{ Inner }

type Names []string

func (n Names) Join() string { return strings.Join(n, "+") }

type Joiner interface{ Join() string }

func run() string {
	var lines []string
	d := Dog{Base{"rex"}, "woof"}
	lines = append(lines, fmt.Sprint(d.Name(), " ", d.Sound(), " ", d.Name()))
	var n Namer = d
	lines = append(lines, fmt.Sprint(n.Name(), " ", n.(Dog).Sound()))
	p := &d
	p.Rename("max")
	lines = append(lines, fmt.Sprint(p.Name(), " ", p.Sound(), " ", d.Name()))

	var m Doubler = B{T(21)}
	lines = append(lines, fmt.Sprint(m.M(), " ", B{T(4)}.M(), " ", B{T(4)}.T.M()))

	o := Outer{&Inner{7}}
	f := (*Outer).V
	g := Outer.V
	pl := Plain{Inner{9}}
	h := (*Plain).V
	lines = append(lines, fmt.Sprint(f(&o), " ", g(o), " ", o.V(), " ", h(&pl), " ", pl.V()))

	var j Joiner = Names{"a", "b"}
	lines = append(lines, j.Join())

	defer func() {
		if r := recover(); r != nil {
			lines = append(lines, fmt.Sprint("recovered: ", r))
		}
	}()
	var bad Outer
	lines = append(lines, fmt.Sprint(f(&bad)))
	return strings.Join(lines, "\n")
}
