package main

import "fmt"

type node struct {
	id   int
	next *node
}

func run() string {
	table := map[int]*node{}

	var head *node
	for i := 0; i < 2000; i++ {
		n := &node{id: i, next: head}
		head = n
		table[i] = n
	}

	for i := 0; i < 1000; i++ {
		delete(table, i*2)
	}

	live := 0
	sum := 0
	for i := 0; i < 2000; i++ {
		if n, ok := table[i]; ok {
			live++
			sum += n.id
		}
	}

	table[42] = &node{id: 4242}
	replaced := table[42].id

	var nilNode *node
	table[7777] = nilNode
	stored, present := table[7777]

	return fmt.Sprintf("%d %d %d %v %v", live, sum, replaced, stored == nil, present)
}
