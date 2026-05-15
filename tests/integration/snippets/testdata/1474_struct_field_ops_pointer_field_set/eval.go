package main

type S struct{ X int }

func run() int {
	s := &S{}
	s.X = 20
	return s.X
}
