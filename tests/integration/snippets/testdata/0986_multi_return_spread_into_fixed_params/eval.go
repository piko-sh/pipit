package main

import "fmt"

type accumulator struct {
	total int
}

func (a *accumulator) addPair(x, y int) int {
	a.total += x + y
	return a.total
}

func pair() (int, int) {
	return 10, 20
}

func triple() (int, int, int) {
	return 1, 2, 3
}

func mixed() (int, string) {
	return 7, "seven"
}

func sumTwo(a, b int) int {
	return a + b
}

func sumThree(a, b, c int) int {
	return a + b + c
}

func describe(count int, label string) string {
	return fmt.Sprintf("%d=%s", count, label)
}

func boxBoth(a, b any) string {
	return fmt.Sprintf("%v,%v", a, b)
}

func run() string {
	two := sumTwo(pair())

	three := sumThree(triple())

	described := describe(mixed())

	boxed := boxBoth(pair())

	acc := &accumulator{}
	method := acc.addPair(pair())

	return fmt.Sprintf("%d %d %s %s %d", two, three, described, boxed, method)
}
