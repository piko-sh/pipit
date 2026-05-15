package main

import "fmt"

func f() (r []int) {
	defer func() { r = append(r, 99) }()
	r = []int{1, 2}
	return r
}

func run() string {
	xs := f()
	out := ""
	for _, v := range xs {
		out += fmt.Sprintf("%d,", v)
	}
	return out
}
