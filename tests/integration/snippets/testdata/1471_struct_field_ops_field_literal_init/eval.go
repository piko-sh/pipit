package main

type S struct{ X int }

func run() int {
	s := S{X: 42}
	return s.X
}
