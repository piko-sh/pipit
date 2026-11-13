package main

import (
	"fmt"
	"strings"
	"unsafe"
)

type pair struct {
	a, b uint32
}

type cell struct{ v int }

func wordsOf(p *pair) []uint32 {
	return unsafe.Slice((*uint32)(unsafe.Pointer(p)), 2)
}

func escaped() []uint32 {
	p := &pair{a: 11, b: 12}
	words := wordsOf(p)
	words[0] = 21
	return words
}

func churn() int {
	total := 0
	for i := 0; i < 2000; i++ {
		c := &pair{a: uint32(i), b: uint32(i * 2)}
		total += int(c.a + c.b)
	}
	return total
}

func run() string {
	p := &pair{a: 1, b: 2}
	words := wordsOf(p)
	words[1] = 9
	p.a = 7

	cs := []cell{{1}, {2}, {3}}
	view := unsafe.Slice(&cs[0], 3)
	view[2].v = 30
	cs[0].v = 10

	arr := [4]byte{1, 2, 3, 4}
	b := unsafe.Slice(&arr[0], 4)
	b[3] = 40
	arr[0] = 10

	x := 5
	one := unsafe.Slice(&x, 1)
	one[0] = 6

	kept := escaped()
	work := churn()
	kept[1]++

	var none *int
	empty := unsafe.Slice(none, 0)
	y := 3
	zero := unsafe.Slice(&y, 0)

	lines := []string{
		fmt.Sprint(p.a, " ", p.b, " ", words),
		fmt.Sprint(cs, view),
		fmt.Sprint(arr, b),
		fmt.Sprint(x, one),
		fmt.Sprint(kept, work),
		fmt.Sprint(len(empty), empty == nil, len(zero), zero == nil),
	}
	return strings.Join(lines, "\n")
}
