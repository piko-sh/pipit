package main

func f() int {
	x := 42
	g := func() int { return x }
	return g()
}

func run() int {
	return f()
}
