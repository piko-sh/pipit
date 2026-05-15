package main

import "fmt"

type box struct {
	items []int
}

func run() string {
	b := box{items: []int{1, 2, 3}}
	return fmt.Sprintf("%d %v", b.items[0], b.items)
}
