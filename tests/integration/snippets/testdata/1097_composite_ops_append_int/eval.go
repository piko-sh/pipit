package main

func run() int {
	s := []int{1}
	s = append(s, 2, 3)
	return s[2]
}
