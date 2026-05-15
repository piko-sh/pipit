package main

func makeCounter() func() int {
	n := 0
	return func() int { n++; return n }
}

func run() int {
	c := makeCounter()
	c()
	c()
	return c()
}
