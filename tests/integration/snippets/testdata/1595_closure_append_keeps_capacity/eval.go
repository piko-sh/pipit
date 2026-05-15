package main

import "fmt"

func run() string {
	out := make([]byte, 0, 8)
	before := cap(out)
	f := func() {
		out = append(out, 'x')
	}
	f()
	return fmt.Sprint(before, " ", len(out), " ", cap(out))
}
