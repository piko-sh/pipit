package main

func run() int {
	sum := 0
	for i := 0; i < 5; i++ {
		for j := 0; j < 5; j++ {
			sum++
		}
	}
	return sum
}
