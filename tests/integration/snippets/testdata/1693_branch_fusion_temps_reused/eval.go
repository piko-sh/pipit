package main

import "fmt"

func matchWord(s string) string {
	const word = "go"
	if s == word {
		return "hit:" + word
	}
	return "miss:" + word
}

func nilOrNot(p *int) string {
	var none *int
	if p == none {
		return fmt.Sprint(none == nil)
	}
	return "set"
}

func countUp(limit int) int {
	i, steps := 0, 0
	for {
		i++
		lt := i < limit
		if !lt {
			break
		}
		steps++
		_ = lt
	}
	return steps
}

func shortPrefix(s string, i int) int {
	n := len(s)
	if i < n {
		return n * 10
	}
	return n
}

func classify(b byte) uint {
	const seven = uint(7)
	v := uint(b)
	if v == seven {
		return seven * 100
	}
	return seven
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func run() string {
	x := 5
	digits := 0
	for i := 0; i < 256; i++ {
		if isDigit(byte(i)) {
			digits++
		}
	}
	return fmt.Sprint(matchWord("go"), matchWord("no"), nilOrNot(nil), nilOrNot(&x),
		countUp(5), shortPrefix("abc", 1), shortPrefix("abc", 9), classify(7), classify(8), digits)
}
