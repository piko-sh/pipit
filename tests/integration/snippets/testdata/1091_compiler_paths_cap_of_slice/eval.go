package main

func run() int {
	s := make([]int, 3, 10)
	return cap(s)
}
