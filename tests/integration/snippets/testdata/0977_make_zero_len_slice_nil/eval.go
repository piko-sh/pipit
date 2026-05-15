package main

import "fmt"

type pair struct {
	a int
	b int
}

type intList []int

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

func probe(s []int) bool {
	return s == nil
}

func run() string {
	makeInt0 := make([]int, 0)
	makeInt00 := make([]int, 0, 0)
	makeInt0Cap := make([]int, 0, 4)
	makeStr0 := make([]string, 0)
	makeByte0 := make([]byte, 0)
	makeFloat0 := make([]float64, 0)
	makeBool0 := make([]bool, 0)
	makeUint0 := make([]uint64, 0)
	makeInt320 := make([]int32, 0)
	makePair0 := make([]pair, 0)
	makeNamed0 := make(intList, 0)
	emptyLit := []int{}

	var varNil []int
	nilConv := []int(nil)
	resliceNil := varNil[:0]
	nonNil := []int{7}
	resliceNonNil := nonNil[:0]
	resliceTail := nonNil[1:]
	appendOnNil := append(varNil, 1)
	appendZeroOnNil := append(varNil, resliceNil...)
	appendZeroOnMake := append(makeInt0, varNil...)
	var zeroArr [0]int
	zeroArrSlice := zeroArr[:]
	fullArr := [3]int{1, 2, 3}
	arrTail := fullArr[3:]
	emptyConv := []byte("")

	results := []bool{
		makeInt0 == nil, makeInt00 == nil, makeInt0Cap == nil,
		makeStr0 == nil, makeByte0 == nil, makeFloat0 == nil,
		makeBool0 == nil, makeUint0 == nil, makeInt320 == nil,
		makePair0 == nil, makeNamed0 == nil, emptyLit == nil,
		varNil == nil, nilConv == nil, resliceNil == nil,
		resliceNonNil == nil, resliceTail == nil,
		appendOnNil == nil, appendZeroOnNil == nil, appendZeroOnMake == nil,
		zeroArrSlice == nil, arrTail == nil, emptyConv == nil,
	}

	branches := 0
	if makeInt0 != nil {
		branches++
	}
	if varNil == nil {
		branches += 2
	}
	if makeByte0 == nil {
		branches += 4
	}
	probes := 0
	if probe(makeInt0) {
		probes++
	}
	if probe(varNil) {
		probes += 2
	}

	total := len(makeInt0) + cap(makeInt0Cap) + len(appendOnNil)
	return fmt.Sprintf("%s %d %d %d", flags(results), branches, probes, total)
}
