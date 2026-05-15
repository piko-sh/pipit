package main

func run() int {
	count := 0
	for range "hello" {
		count++
	}
	return count
}
