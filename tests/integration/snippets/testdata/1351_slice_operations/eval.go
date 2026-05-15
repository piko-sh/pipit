package main

func run() int {
	s := make([]int, 0)
	s = append(s, 1, 2, 3)
	return s[0] + s[1] + s[2]
}
