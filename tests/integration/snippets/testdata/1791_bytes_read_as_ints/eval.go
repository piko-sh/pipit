package main

import (
	"fmt"
	"strings"
)

var table = []int{10, 20, 30, 40}
var global []byte

func caught(body func()) (message string) {
	defer func() {
		if r := recover(); r != nil {
			message = fmt.Sprint(r)
		}
	}()
	body()
	return "no panic"
}

func sum(source []byte) int {
	total := 0
	for i := 0; i < len(source); i++ {
		total += int(source[i])
	}
	return total
}

func lookup(source []byte) int {
	total := 0
	for i := 0; i < len(source); i++ {
		total += table[int(source[i])&3]
	}
	return total
}

func remap(source, colours []byte) []int {
	out := []int{}
	for i := range source {
		out = append(out, int(colours[int(source[i])]))
	}
	return out
}

func both(source []byte) (int, uint8) {
	total, highest := 0, uint8(0)
	for i := 0; i < len(source); i++ {
		b := source[i]
		total += int(b)
		if b > highest {
			highest = b
		}
	}
	return total, highest
}

func signed(source []byte) int {
	total := 0
	for i := 0; i < len(source); i++ {
		total += int(int8(source[i]))
	}
	return total
}

func swapped(n int) int {
	total := 0
	for i := 0; i < n; i++ {
		total += int(global[i])
		if i == 1 {
			global = []byte{9, 9, 9, 200}
		}
	}
	return total
}

func once(source []byte, at int) int {
	return int(source[at]) + 1
}

func wide(source []byte) uint64 {
	var value uint64
	for i := 0; i < len(source); i++ {
		value = value<<8 | uint64(source[i])
	}
	return value
}

func run() string {
	high := []byte{0, 1, 127, 128, 200, 255}
	total, highest := both(high)
	global = []byte{1, 2, 3, 4}
	lines := []string{
		fmt.Sprint("sum ", sum(high), " ", sum(nil)),
		fmt.Sprint("lookup ", lookup(high)),
		fmt.Sprint("remap ", remap([]byte{2, 0, 1}, []byte{250, 128, 7})),
		fmt.Sprint("both ", total, " ", highest),
		fmt.Sprint("signed ", signed(high)),
		fmt.Sprint("swapped ", swapped(4)),
		fmt.Sprint("once ", once(high, 5), " ", caught(func() { once(high, 6) }), " ", caught(func() { once(high, -1) })),
		fmt.Sprint("wide ", wide(high)),
		"remap past the end " + caught(func() { remap([]byte{3}, []byte{1, 2, 3}) }),
	}
	return strings.Join(lines, "\n")
}
