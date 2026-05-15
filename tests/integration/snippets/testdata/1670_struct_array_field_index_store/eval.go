package main

import (
	"fmt"
	"strings"
)

type boxed struct {
	ints    [32]int
	floats  [8]float64
	strs    [8]string
	bools   [8]bool
	uints   [8]uint
	nested  [4]inner
	scalar  int
	swapped [4]int
}

type inner struct {
	n int
}

func twoStores() string {
	var p boxed
	src := [4]int{4, 2, 3, 5}
	file := 0
	p.ints[file] = src[file]
	p.ints[16+file] = 1
	return fmt.Sprint(p.ints[0], " ", p.ints[16])
}

func storeThenRead() string {
	var p boxed
	p.ints[1] = 9
	first := p.ints[1]
	p.ints[2] = first + 1
	return fmt.Sprint(first, " ", p.ints[2])
}

func readStoreRead() string {
	var p boxed
	p.ints[3] = 5
	a := p.ints[3]
	p.ints[4] = 7
	return fmt.Sprint(a, " ", p.ints[4])
}

func everyElementKind() string {
	var p boxed
	i := 0
	p.floats[i] = 1.5
	p.floats[2+i] = 2.5
	p.strs[i] = "a"
	p.strs[2+i] = "b"
	p.bools[i] = true
	p.bools[2+i] = true
	p.uints[i] = 11
	p.uints[2+i] = 22
	return fmt.Sprint(p.floats[0], " ", p.floats[2], " ", p.strs[0], p.strs[2], " ",
		p.bools[0], p.bools[2], " ", p.uints[0], " ", p.uints[2])
}

func nestedStructElements() string {
	var p boxed
	k := 0
	p.nested[k].n = 3
	p.nested[1+k].n = 4
	return fmt.Sprint(p.nested[0].n, " ", p.nested[1].n)
}

func loopStores() string {
	var p boxed
	for i := 0; i < 4; i++ {
		p.swapped[i] = i
		p.ints[24+i] = i * 2
	}
	return fmt.Sprint(p.swapped, " ", p.ints[24], p.ints[25], p.ints[26], p.ints[27])
}

func run() string {
	return strings.Join([]string{
		twoStores(), storeThenRead(), readStoreRead(),
		everyElementKind(), nestedStructElements(), loopStores(),
	}, "\n")
}
