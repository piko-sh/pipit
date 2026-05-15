package main

type S struct{ X int }

func run() int {
	s := S{}
	s.X = 5
	return s.X
}
