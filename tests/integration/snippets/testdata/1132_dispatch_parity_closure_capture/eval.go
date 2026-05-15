package main

func run() int {
	x := 10
	f := func() int { return x + 5 }
	return f()
}
