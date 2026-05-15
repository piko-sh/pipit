package main

func swap(a, b int) (int, int) { return b, a }

func run() int {
	x, y := swap(1, 2)
	return x*10 + y
}
