package main

import "fmt"

func run() string {
	ch := make(chan int, 4)
	ch <- 1
	ch <- 2
	ch <- 3
	ch <- 4
	total := 0
	rounds := 0
outer:
	for {
		rounds++
		select {
		case v := <-ch:
			if v == 2 {
				break
			}
			if v == 4 {
				break outer
			}
			total += v
		default:
			break outer
		}
		total += 10
	}
	return fmt.Sprint(total, " ", rounds)
}
