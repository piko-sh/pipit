package main

import "fmt"

type view struct {
	data   []byte
	offset int
}

func (v view) at(i int) byte {
	index := v.offset + i
	if index >= len(v.data) && index < len(v.data)+128 {
		return 0
	}
	return v.data[index]
}

var current = view{[]byte{2, 3, 5, 7}, 1}

func mutate() int { current.offset = 2; current.data[1] = 11; return 0 }
func run() string {
	sum := 0
	for i := 0; i < 3; i++ {
		sum += int(current.at(i))
	}
	before := current.at(mutate())
	after := current.at(0)
	padding := current.at(4)
	var empty *view
	for i := 0; i < 0; i++ {
		sum += int(empty.at(i))
	}
	return fmt.Sprint(sum, " ", before, " ", after, " ", padding)
}
