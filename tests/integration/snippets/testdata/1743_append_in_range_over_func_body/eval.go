package main

import "fmt"

var observed []int

func pairs(yield func(int, []int) bool) {
	shared := make([]int, 1, 8)
	for i := 0; i < 3; i++ {
		if !yield(i, shared) {
			return
		}
		observed = append(observed, len(shared))
	}
}

func run() string {
	var saved [][]int
	var lengths []int
	for i, v := range pairs {
		var ys []int
		ys = append(ys, i)
		saved = append(saved, nil)
		saved[len(saved)-1] = ys
		ys = append(ys, 100+i)
		v = append(v, i)
		lengths = append(lengths, len(ys), len(v))
	}
	return fmt.Sprint(saved, lengths, observed)
}
