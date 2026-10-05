package main

import (
	"fmt"
	"math"
)

type fixed = int32

func counters() string {
	var up, down int32 = math.MaxInt32 - 2, math.MinInt32 + 2
	for range 5 {
		up++
		down--
	}
	return fmt.Sprint(up, " ", down)
}

func constants(n int) string {
	var a, b int32 = math.MaxInt32 - 100, math.MinInt32 + 100
	for i := 0; i < n; i++ {
		a += 37
		b -= 41
	}
	return fmt.Sprint(a, " ", b)
}

func products(steps []int32) string {
	var sum, product fixed = math.MaxInt32, 7
	var difference int32 = math.MinInt32
	for _, step := range steps {
		sum += step
		difference -= step
		product *= step
	}
	return fmt.Sprint(sum, " ", difference, " ", product)
}

func unsigned(steps []uint32) uint32 {
	var position uint32 = math.MaxUint32 - 5
	for _, step := range steps {
		position += step
	}
	return position
}

func maskedAdd(a, b uint32) uint32 { return a + b }

func run() string {
	return fmt.Sprint(
		counters(), " | ",
		constants(70000000), " | ",
		products([]int32{3, 1 << 20, -7, 1 << 30, 65537}), " | ",
		unsigned([]uint32{3, 9, math.MaxUint32, 1 << 31}), " | ",
		maskedAdd(math.MaxUint32, 2),
	)
}
