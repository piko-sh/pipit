package main

type S struct{ X int }

func (s *S) Set(v int) { s.X = v }

func (s S) Get() int { return s.X }

func run() int {
	s := S{X: 0}
	s.Set(99)
	return s.Get()
}
