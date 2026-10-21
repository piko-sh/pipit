package main

import "fmt"

type small uint8

type signed int8

func sum[T ~uint8](a, b T) T {
	a += b
	return a | 1
}

func halved[T ~uint8 | ~uint16](a, b T) T {
	a += b
	return a >> 1
}

func masked[T ~uint8](a, b T) int {
	a += b
	a |= 1
	return int(a)
}

func product[T ~int8](a, b T) int64 {
	a *= b
	a &= 0x7f
	return int64(a) + 1000
}

func low[T ~uint8](x int) T {
	return T(x)
}

func lowThenOr[T ~uint8](x int) int {
	y := T(x)
	return int(y | 1)
}

func between[T ~uint8, U ~uint16](x U) uint32 {
	y := T(x)
	return uint32(y) + 5
}

func signedBetween[T ~int8, U ~int32](x U) int64 {
	return int64(T(x))
}

func widened[T ~uint32, U ~uint8](x U) T {
	return T(x) + 1
}

func stepped[T ~uint8](a T) int {
	a++
	a |= 2
	return int(a)
}

func steppedDown[T ~int8](a T) int {
	a--
	return int(a) % 7
}

func over[T ~uint8](a, b T) bool {
	a += b
	return a > 100
}

func folded[T ~uint8](n int) int {
	var acc T
	for i := 0; i < n; i++ {
		acc += T(i)
		acc ^= 3
	}
	return int(acc)
}

func counter[T ~uint8](start T) func(T) T {
	total := start
	return func(by T) T {
		total += by
		return total >> 1
	}
}

func run() string {
	count := counter[uint8](250)
	return fmt.Sprint(
		sum[uint8](200, 100), halved[uint8](200, 100), halved[uint16](60000, 10000), masked[small](200, 100), product[int8](100, 3), "\n",
		low[uint8](300), lowThenOr[small](300), between[uint8, uint16](0x1234), signedBetween[signed, int32](0x123456ff),
		widened[uint32, uint8](255), "\n",
		stepped[uint8](255), steppedDown[int8](-128), over[uint8](200, 100), folded[uint8](1000), folded[small](777), "\n",
		count(10), count(250), count(3))
}
