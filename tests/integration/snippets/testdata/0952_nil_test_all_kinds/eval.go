package main

import "fmt"

type node struct {
	next *node
}

func flags(results []bool) string {
	out := ""
	for _, r := range results {
		if r {
			out += "1"
		} else {
			out += "0"
		}
	}
	return out
}

func run() string {
	var nilPtr *node
	livePtr := &node{}
	var nilSlice []int
	liveSlice := []int{1}
	madeSlice := make([]int, 0)
	var nilMap map[string]int
	liveMap := map[string]int{"a": 1}
	var nilChan chan int
	liveChan := make(chan int, 1)
	var nilFunc func() int
	liveFunc := func() int { return 7 }
	var nilIface any
	var typedNilInIface any = nilPtr
	var liveIface any = 42

	results := []bool{
		nilPtr == nil, livePtr == nil,
		nilSlice == nil, liveSlice == nil, madeSlice == nil,
		nilMap == nil, liveMap == nil,
		nilChan == nil, liveChan == nil,
		nilFunc == nil, liveFunc == nil,
		nilIface == nil, typedNilInIface == nil, liveIface == nil,
	}

	branches := 0
	if nilPtr != nil {
		branches += 1
	}
	if livePtr != nil {
		branches += 2
	}
	n := livePtr
	steps := 0
	n.next = &node{}
	for n != nil {
		steps++
		n = n.next
	}
	_ = liveFunc()
	return fmt.Sprintf("%s %d %d", flags(results), branches, steps)
}
