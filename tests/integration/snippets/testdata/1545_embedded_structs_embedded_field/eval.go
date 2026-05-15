package main

type Base struct{ X int }

type Derived struct{ Base }

func run() int {
	d := Derived{Base: Base{X: 42}}
	return d.X
}
