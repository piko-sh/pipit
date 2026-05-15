package main

func getInt() int { return 42 }

func wrap() any { return getInt() }

func run() int {
	x := wrap()
	y := x.(int)
	return y
}
