package main

type S struct{ X int }

func run() int {
	s := S{X: 5}
	s.X++
	return s.X
}
