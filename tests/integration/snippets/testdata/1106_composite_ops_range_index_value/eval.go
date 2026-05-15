package main

func run() int {
	sum := 0
	for _, v := range []int{10, 20, 30} {
		sum += v
	}
	return sum
}
