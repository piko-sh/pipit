package main

import "fmt"

type Count uint16

type cell struct {
	label string
	value int
}

func sized(n byte) []byte {
	return make([]byte, n)
}

func run() string {
	out := ""
	var b byte = 5
	out += fmt.Sprintln(len(sized(b)), cap(sized(b)))

	var u uint = 3
	var u32 uint32 = 9
	ints := make([]int, u, u32)
	out += fmt.Sprintln(len(ints), cap(ints))

	var c Count = 4
	cells := make([]cell, c)
	cells[c-1] = cell{label: "last", value: int(c)}
	out += fmt.Sprintln(len(cells), cells[c-1])

	var small int8 = 2
	strs := make([]string, small, uint64(small)*3)
	out += fmt.Sprintln(len(strs), cap(strs))

	var buffered uint8 = 2
	ch := make(chan int, buffered)
	ch <- 1
	ch <- 2
	out += fmt.Sprintln(len(ch), cap(ch))

	for i := byte(0); i < 3; i++ {
		row := make([]uint16, i, i+1)
		out += fmt.Sprintln(i, len(row), cap(row))
	}

	m := make(map[string]int, u32)
	m["k"] = 1
	out += fmt.Sprintln(len(m))

	return out
}
