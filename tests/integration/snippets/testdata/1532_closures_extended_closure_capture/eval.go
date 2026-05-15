package main

func make() func() int {
	x := 15
	return func() int { return x }
}

func run() int {
	f := make()
	return f()
}
