package main

func run() int {
	ch := make(chan int, 3)
	ch <- 10
	ch <- 20
	ch <- 30
	close(ch)
	fns := make([]func() int, 3)
	index := 0
	for v := range ch {
		v := v
		i := index
		fns[i] = func() int { return v }
		index++
	}
	return fns[0]() + fns[1]() + fns[2]()
}
