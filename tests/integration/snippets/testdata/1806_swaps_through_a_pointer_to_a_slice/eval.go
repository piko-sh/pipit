package main

import "fmt"

func swap(values *[]int, i, j int) {
	(*values)[i], (*values)[j] = (*values)[j], (*values)[i]
}

func swapWide(values *[]int64, i, j int) {
	(*values)[i], (*values)[j] = (*values)[j], (*values)[i]
}

func swapNarrow(values *[]int16, i, j int) {
	(*values)[i], (*values)[j] = (*values)[j], (*values)[i]
}

func rotate(values *[]int, i, j, k int) {
	(*values)[i], (*values)[j], (*values)[k] = (*values)[j], (*values)[k], (*values)[i]
}

func swapAcross(left, right *[]int, i int) {
	(*left)[i], (*right)[i] = (*right)[i], (*left)[i]
}

func grow(values *[]int) int {
	*values = append(*values, 99)
	return len(*values)
}

func storeAfterGrowing(values *[]int) {
	(*values)[0], (*values)[1] = grow(values), 7
}

func siftUp(heap, keys *[]int, position int) {
	for position > 0 {
		parent := (position - 1) / 2
		if (*keys)[parent] <= (*keys)[position] {
			break
		}
		(*heap)[parent], (*heap)[position] = (*heap)[position], (*heap)[parent]
		(*keys)[parent], (*keys)[position] = (*keys)[position], (*keys)[parent]
		position = parent
	}
}

func caught(body func()) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprint(r)
		}
	}()
	body()
	return "ok"
}

func run() string {
	plain := []int{1, 2, 3, 4}
	wide := []int64{10, 20, 30}
	narrow := []int16{-1, 2, -3}
	other := []int{7, 8, 9, 10}
	swap(&plain, 0, 3)
	swapWide(&wide, 0, 2)
	swapNarrow(&narrow, 0, 2)
	rotate(&plain, 0, 1, 2)
	swapAcross(&plain, &other, 1)

	grown := make([]int, 2, 2)
	storeAfterGrowing(&grown)

	heap := []int{0, 1, 2, 3, 4}
	keys := []int{50, 40, 30, 20, 10}
	siftUp(&heap, &keys, 4)
	siftUp(&heap, &keys, 3)

	var none *[]int
	short := []int{1}
	return fmt.Sprint(
		plain, wide, narrow, other, grown, heap, keys, " ",
		caught(func() { swap(none, 0, 1) }), " | ",
		caught(func() { swap(&short, 0, 1) }), " | ",
		caught(func() { swap(&short, -1, 0) }), " ", short,
	)
}
