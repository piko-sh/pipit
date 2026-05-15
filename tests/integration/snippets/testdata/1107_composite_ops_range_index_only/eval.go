package main

func run() int {
	last := 0
	for i := range []int{10, 20, 30} {
		last = i
	}
	return last
}
