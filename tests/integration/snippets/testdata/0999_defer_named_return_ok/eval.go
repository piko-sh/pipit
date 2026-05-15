package main

import "fmt"

func scalarReassign() (r int) {
	defer func() { r = 99 }()
	r = 5
	return r
}

func scalarDouble() (r int) {
	defer func() { r *= 2 }()
	r = 21
	return r
}

func directSliceAppend() []int {
	r := []int{1, 2}
	r = append(r, 99)
	return r
}

func run() string {
	xs := directSliceAppend()
	sum := 0
	for _, v := range xs {
		sum += v
	}
	return fmt.Sprintf("%d %d %d", scalarReassign(), scalarDouble(), sum)
}
