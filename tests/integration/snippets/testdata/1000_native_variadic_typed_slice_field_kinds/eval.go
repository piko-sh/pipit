package main

import "fmt"

type bag struct {
	ints    []int
	floats  []float64
	strings []string
	bools   []bool
	bytes   []byte
}

func run() string {
	b := bag{
		ints:    []int{1, 2, 3},
		floats:  []float64{1.5, 2.5},
		strings: []string{"a", "b"},
		bools:   []bool{true, false},
		bytes:   []byte{7, 8},
	}
	return fmt.Sprintf("%v %v %v %v %v", b.ints, b.floats, b.strings, b.bools, b.bytes)
}
