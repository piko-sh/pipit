package main

type S struct{ X int }

func run() int {
	s := S{X: 3}
	s.X *= 2
	return s.X
}
