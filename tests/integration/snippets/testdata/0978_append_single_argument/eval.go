package main

import "fmt"

func run() string {
	var nilSlice []int
	kept := append(nilSlice)
	madeSlice := make([]int, 0)
	keptMade := append(madeSlice)
	grown := append(append(nilSlice), 1, 2)
	return fmt.Sprintf("%t %t %t %d %d",
		kept == nil, keptMade == nil, grown == nil, len(grown), grown[1])
}
