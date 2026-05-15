package main

func run() int {
	sum := 0
	for i := 0; i < 100; i++ {
		if i >= 5 {
			break
		}
		sum++
	}
	return sum
}
