package main

type Inner struct{ X int }

type Outer struct{ I Inner }

func run() int {
	o := Outer{I: Inner{X: 42}}
	return o.I.X
}
