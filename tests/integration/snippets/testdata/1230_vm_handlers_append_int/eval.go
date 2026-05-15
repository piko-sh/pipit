package main

func run() int {
	s := []int{1, 2}
	s = append(s, 3, 4)
	return len(s)
}
