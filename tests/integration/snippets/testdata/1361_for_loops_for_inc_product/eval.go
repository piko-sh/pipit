package main

func run() int {
	p := 1
	for i := 1; i <= 10; i++ {
		p *= i
	}
	return p
}
