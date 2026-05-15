package main

import "fmt"

func run() string {
	out := make([]int, 0, 4)
	grow := func() {
		out = append(out, 1)
	}
	observe := func(s []int) int {
		grow()
		return len(s)
	}
	return fmt.Sprint(observe(out), " ", len(out))
}
