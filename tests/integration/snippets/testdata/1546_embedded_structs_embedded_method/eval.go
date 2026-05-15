package main

type Base struct{ X int }

func (b Base) Value() int { return b.X }

type Derived struct{ Base }

func run() int {
	d := Derived{Base: Base{X: 99}}
	return d.Value()
}
