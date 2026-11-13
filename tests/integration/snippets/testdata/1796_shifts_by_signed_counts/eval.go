package main

import (
	"fmt"
	"strings"
)

type count int8

func caught(body func()) (message string) {
	defer func() {
		if r := recover(); r != nil {
			message = fmt.Sprint(r)
		}
	}()
	body()
	return "no panic"
}

func left(x, n int) int               { return x << n }
func right(x, n int) int              { return x >> n }
func leftUnsigned(x uint, n int) uint { return x << n }
func narrow(x int32, n count) int32   { return x >> n }
func byUnsigned(x int, n uint) int    { return x << n }

func compound(x, n int) int {
	x <<= n
	x >>= n / 2
	return x
}

func inLoop(n int) int {
	total := 0
	for i := 0; i < 70; i++ {
		total += (1 << (i - n)) & 0xff
	}
	return total
}

func run() string {
	lines := []string{
		fmt.Sprint(left(3, 4), right(-256, 3), leftUnsigned(5, 2), narrow(-1024, 3), byUnsigned(7, 70), compound(9, 6)),
		fmt.Sprint(left(1, 64), right(-1, 100), left(1, 0)),
		caught(func() { left(1, -1) }),
		caught(func() { right(1, -64) }),
		caught(func() { leftUnsigned(1, -2) }),
		caught(func() { narrow(1, -3) }),
		caught(func() { compound(1, -2) }),
		fmt.Sprint(inLoop(0)),
		caught(func() { inLoop(5) }),
	}
	return strings.Join(lines, "\n")
}
