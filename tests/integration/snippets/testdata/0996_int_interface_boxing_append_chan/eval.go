package main

import "fmt"

func run() string {
	var xs []interface{}
	xs = append(xs, 7)

	ch := make(chan interface{}, 1)
	ch <- 9
	recv := <-ch

	return fmt.Sprintf("%T %T", xs[0], recv)
}
