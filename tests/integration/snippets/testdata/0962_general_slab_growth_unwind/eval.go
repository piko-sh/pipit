package main

import "fmt"

type node struct {
	value int
	next  *node
}

func deepSum(n *node, depth int) int {
	if n == nil || depth == 0 {
		return 0
	}
	return n.value + deepSum(n.next, depth-1)
}

func build(count int) *node {
	var head *node
	for i := count; i > 0; i-- {
		head = &node{value: i, next: head}
	}
	return head
}

func run() string {
	head := build(3000)
	first := deepSum(head, 3000)
	second := deepSum(head, 1500)
	return fmt.Sprintf("%d %d", first, second)
}
