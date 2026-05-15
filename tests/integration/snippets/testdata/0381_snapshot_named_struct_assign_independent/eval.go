package main

type Wide struct {
	A, B, C, D int
}

func run() int {
	source := Wide{A: 1, B: 2, C: 3, D: 4}
	destination := source
	destination.A = 99
	return source.A*1000 + destination.A
}
