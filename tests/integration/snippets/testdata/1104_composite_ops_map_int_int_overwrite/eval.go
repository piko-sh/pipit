package main

func run() int {
	m := map[int]int{1: 10}
	m[1] = 99
	return m[1]
}
