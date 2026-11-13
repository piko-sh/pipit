package main

import "fmt"

type item struct {
	weight int
	ratio  float64
	label  string
}

var calls int

func nextByte() uint8 {
	calls++
	return uint8(calls % 3)
}

func nextWide() uint32 {
	calls += 10
	return 2
}

func nextInt() int {
	calls += 100
	return 1
}

func run() string {
	items := []item{{weight: 5, ratio: 0.5, label: "a"}, {weight: 7, ratio: 1.5, label: "b"}, {weight: 9, ratio: 2.5, label: "c"}}
	first := items[nextByte()].weight
	second := items[nextWide()].ratio
	third := items[nextInt()].label
	total := 0
	for i := uint16(0); i < 3; i++ {
		total += items[i].weight
	}
	var index uint64 = 2
	return fmt.Sprint(first, second, third, calls, total, items[index].weight)
}
