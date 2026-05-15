package main

func f() (a int, b int) {
	a = 10
	b = 20
	return
}

func run() int {
	x, y := f()
	return x + y
}
