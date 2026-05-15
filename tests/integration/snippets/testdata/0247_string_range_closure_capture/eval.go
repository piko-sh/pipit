package main

func run() int {
	fns := make([]func() int, 3)
	index := 0
	for _, r := range "abc" {
		r := r
		i := index
		fns[i] = func() int { return int(r) }
		index++
	}
	return fns[0]() + fns[1]() + fns[2]()
}
