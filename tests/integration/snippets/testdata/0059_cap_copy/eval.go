package main

func run() int {
	source := []int{1, 2, 3, 4, 5}
	destination := make([]int, 3)
	n := copy(destination, source)
	return destination[0] + destination[1] + destination[2] + n
}
