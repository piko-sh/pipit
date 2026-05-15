package main

func run() int {
	x := 10
	p := &x
	*p = 20
	return x
}
