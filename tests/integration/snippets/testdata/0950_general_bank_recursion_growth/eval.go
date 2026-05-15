package main

import "fmt"

type node struct {
	depth int
	score float64
	seen  bool
	left  *node
	right *node
}

func build(depth int) *node {
	if depth == 0 {
		return nil
	}
	return &node{
		depth: depth,
		score: float64(depth) * 0.5,
		left:  build(depth - 1),
		right: build(depth - 1),
	}
}

func walk(n *node, extra *node, acc int) int {
	if n == nil {
		return acc
	}
	n.seen = true
	n.score += 1.25
	acc += n.depth
	if extra != nil && extra.seen {
		acc++
	}
	acc = walk(n.left, n, acc)
	acc = walk(n.right, n, acc)
	return acc
}

func run() string {
	root := build(12)
	total := walk(root, nil, 0)
	var fsum float64
	var count int
	stack := []*node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		fsum += n.score
		if n.seen {
			count++
		}
		stack = append(stack, n.left, n.right)
	}
	return fmt.Sprintf("%d %.2f %d", total, fsum, count)
}
