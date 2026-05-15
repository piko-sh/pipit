package main

import "fmt"

type item struct {
	weight int
	hits   int
	left   *item
	right  *item
}

func grow(depth int) *item {
	if depth == 0 {
		return nil
	}
	return &item{
		weight: depth,
		left:   grow(depth - 1),
		right:  grow(depth - 1),
	}
}

func run() string {
	root := grow(6)

	visited := 0
	weightSum := 0
	stack := []*item{root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if node == nil {
			continue
		}
		node.hits++
		visited++
		weightSum += node.weight
		stack = append(stack, node.left, node.right)
	}

	reHits := 0
	stack = []*item{root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if node == nil {
			continue
		}
		reHits += node.hits
		stack = append(stack, node.left, node.right)
	}

	return fmt.Sprintf("%d %d %d", visited, weightSum, reHits)
}
