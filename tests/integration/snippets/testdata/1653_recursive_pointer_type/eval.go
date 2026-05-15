package main

import (
	"fmt"
	"strings"
)

type Number *Number

func zero() *Number { return nil }

func isZero(x *Number) bool { return x == nil }

func add1(x *Number) *Number {
	e := new(Number)
	*e = x
	return e
}

func sub1(x *Number) *Number { return *x }

func add(x, y *Number) *Number {
	if isZero(y) {
		return x
	}
	return add(add1(x), sub1(y))
}

func mul(x, y *Number) *Number {
	if isZero(x) || isZero(y) {
		return zero()
	}
	return add(mul(x, sub1(y)), x)
}

func gen(n int) *Number {
	if n > 0 {
		return add1(gen(n - 1))
	}
	return zero()
}

func count(x *Number) int {
	if isZero(x) {
		return 0
	}
	return count(*x) + 1
}

func run() string {
	var lines []string
	three, four := gen(3), gen(4)
	lines = append(lines, fmt.Sprint(count(three), count(four), count(add(three, four)), count(mul(three, four))))
	lines = append(lines, fmt.Sprint(isZero(zero()), isZero(add1(zero())), isZero(sub1(add1(zero())))))
	five := add1(four)
	lines = append(lines, fmt.Sprint(count(five), count(sub1(sub1(five))), count(gen(0))))
	return strings.Join(lines, "\n")
}
