package main

type node struct {
	next  *node
	value int
}

func run() int {
	tail := &node{next: nil, value: 7}
	head := &node{next: tail, value: 1}
	total := 0
	for cursor := head; cursor != nil; cursor = cursor.next {
		total += cursor.value
	}
	return total
}
