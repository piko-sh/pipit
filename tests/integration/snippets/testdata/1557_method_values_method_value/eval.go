package main

type S struct{ X int }

func (s S) Get() int { return s.X }

func run() int {
	s := S{X: 42}
	f := s.Get
	return f()
}
