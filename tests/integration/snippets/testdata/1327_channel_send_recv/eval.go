package main

func run() int {
	ch := make(chan int, 1)
	ch <- 42
	return <-ch
}
