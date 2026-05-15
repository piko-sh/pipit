package main

func run() int {
	m := map[int]int{1: 10}
	m[2] = 20
	return m[1] + m[2]
}
