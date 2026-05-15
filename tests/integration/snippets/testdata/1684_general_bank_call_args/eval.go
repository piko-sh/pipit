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

type pair struct {
	a, b int
}

type shape interface {
	area() int
}

type rect struct{ w, h int }

func (r rect) area() int { return r.w * r.h }

// build makes a tree by recursion; every call passes and returns pointers.
func build(depth, value int) *node {
	if depth == 0 {
		return nil
	}
	return &node{value: value, left: build(depth-1, value*2), right: build(depth-1, value*2+1)}
}

// sum recurses with a pointer argument, the general-bank shape of the fast arm.
func sum(n *node) int {
	if n == nil {
		return 0
	}
	return n.value + sum(n.left) + sum(n.right)
}

// describe takes an interface argument and a nil-able pointer.
func describe(s shape, n *node) string {
	if n == nil {
		return "nil:" + strconv.Itoa(s.area())
	}
	return strconv.Itoa(n.value) + ":" + strconv.Itoa(s.area())
}

// bump takes a struct by value: the fast arm must decline and let the Go trampoline copy
// it, so the caller's copy stays independent.
func bump(p pair) int {
	p.a += 100
	p.b += 100
	return p.a + p.b
}

// bumpArray takes an array by value, the other kind that needs the boundary copy.
func bumpArray(xs [3]int) int {
	xs[0] += 1000
	return xs[0] + xs[1] + xs[2]
}

// viaClosure makes a closure call inside a callee that received a pointer, so a tier-2
// handler runs above a bank the assembly pushed.
func viaClosure(n *node, f func(int) int) int {
	if n == nil {
		return f(0)
	}
	return f(n.value) + viaClosure(n.left, f)
}

func (n *node) mirror() {
	if n == nil {
		return
	}
	n.left, n.right = n.right, n.left
	n.left.mirror()
	n.right.mirror()
}

func run() string {
	out := ""
	for round := 0; round < 3; round++ {
		root := build(5, 1)
		out += strconv.Itoa(sum(root)) + ","
		root.mirror()
		out += strconv.Itoa(sum(root.left)) + "," + strconv.Itoa(sum(root.right)) + ","
		if round == 1 {
			runtime.GC()
		}
		p := pair{a: round, b: 2}
		out += strconv.Itoa(bump(p)) + "/" + strconv.Itoa(p.a+p.b) + ","
		xs := [3]int{round, 2, 3}
		out += strconv.Itoa(bumpArray(xs)) + "/" + strconv.Itoa(xs[0]) + ","
		out += describe(rect{w: 2, h: round + 1}, root) + "," + describe(rect{w: 3, h: 3}, nil) + ","
		scale := round + 2
		out += strconv.Itoa(viaClosure(root, func(v int) int { return v * scale })) + ";"
	}
	return out
}
