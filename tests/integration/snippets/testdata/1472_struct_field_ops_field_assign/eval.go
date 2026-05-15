package main

type S struct{ X int }

func run() int {
	s := S{}
	s.X = 10
	return s.X
}
