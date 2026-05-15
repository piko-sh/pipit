package main

import "fmt"

func run() string {
	var out []int
	out = make([]int, 0, 4)
	var saved []int
	saved = out
	var declared = out
	pair, _ := out, 0
	f := func() {
		out = append(out, 7)
	}
	f()
	saved = append(saved, 9)
	declared = append(declared, 11)
	pair = append(pair, 13)
	return fmt.Sprint(out, saved, declared, pair, len(out), cap(out))
}
