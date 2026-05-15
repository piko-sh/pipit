package main

import (
	"runtime"
	"strconv"
)

type node struct {
	value int
	left  *node
	right *node
}

type box struct {
	items [4]int
	tag   string
}

type shape interface {
	area() int
}

type square struct{ side int }

func (s square) area() int { return s.side * s.side }

// makeTree is the constructor recursion of the tree benchmark: every call returns a
// pointer through the inline return.
func makeTree(depth, seed int) *node {
	if depth == 0 {
		return nil
	}
	return &node{value: seed, left: makeTree(depth-1, seed*2), right: makeTree(depth-1, seed*2+1)}
}

func count(n *node) int {
	if n == nil {
		return 0
	}
	return 1 + count(n.left) + count(n.right)
}

// find returns nil on most paths and a pointer on one.
func find(n *node, value int) *node {
	if n == nil {
		return nil
	}
	if n.value == value {
		return n
	}
	if left := find(n.left, value); left != nil {
		return left
	}
	return find(n.right, value)
}

// makeBox returns a struct by value, held indirectly in the general bank.
func makeBox(seed int) box {
	return box{items: [4]int{seed, seed + 1, seed + 2, seed + 3}, tag: "b" + strconv.Itoa(seed)}
}

// pick returns an interface value holding a struct.
func pick(side int) shape {
	return square{side: side}
}

// choose returns one of two pointers so the returned value changes per call.
func choose(flag bool, a, b *node) *node {
	if flag {
		return a
	}
	return b
}

func run() string {
	out := ""
	for round := 0; round < 3; round++ {
		root := makeTree(6, round+1)
		out += strconv.Itoa(count(root)) + ","
		hit := find(root, (round+1)*8+1)
		if hit != nil {
			out += strconv.Itoa(hit.value) + ","
		} else {
			out += "none,"
		}
		out += strconv.FormatBool(find(root, -1) == nil) + ","
		if round == 1 {
			runtime.GC()
		}
		b := makeBox(round)
		b2 := makeBox(round + 10)
		out += b.tag + strconv.Itoa(b.items[3]) + b2.tag + strconv.Itoa(b2.items[0]) + ","
		out += strconv.Itoa(pick(round+2).area()) + ","
		c := choose(round%2 == 0, root.left, root.right)
		out += strconv.Itoa(c.value) + ";"
	}
	return out
}
