package main

type S struct{ X int }

func run() int {
	s := S{X: 10}
	s.X += 5
	return s.X
}
