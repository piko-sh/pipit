package main

func run() int {
	sum := 0
	for i := 0; i < 20; i += 2 {
		sum += i
	}
	return sum
}
