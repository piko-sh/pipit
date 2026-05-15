package main

func run() int {
	s := []int{1}
	s = append(s, 2, 3, 4, 5)
	return len(s)
}
