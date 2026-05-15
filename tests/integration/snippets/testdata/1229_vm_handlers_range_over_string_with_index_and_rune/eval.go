package main

func run() int {
	sum := 0
	for i, _ := range "abc" {
		sum += i
	}
	return sum
}
