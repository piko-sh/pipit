package main

import "fmt"

func copied(taken bool, a, b int) int {
	if taken {
		a = b
	}
	return (a + 1) * (b + 1)
}

func restored(taken bool, base, other int) int {
	value := base
	first := value + 5
	value = other
	if !taken {
		value = base
	}
	return first*1000 + value + 5
}

func repeated(taken bool, base, other int) int {
	first := base + 5
	second := first + 5
	third := other
	if !taken {
		third = base + 5
	}
	return second*1000 + third + 5
}

func nested(outer, inner bool) int {
	result := 1
	if outer {
		if inner {
			result = 2
		}
	} else {
		result = 3
	}
	return result
}

func nestedUnsigned(outer, inner bool, x, y uint) uint {
	var result uint = 7
	if outer {
		if inner {
			result = 0
		}
	} else {
		result = x + y
	}
	return result
}

func nestedByte(data []byte, outer, inner bool, index int) int {
	var shade byte = 9
	if outer {
		if inner {
			shade = 0
		}
	} else {
		shade = data[index]
	}
	return int(shade) * 3
}

func ladder(score int, strict bool) uint64 {
	var grade uint64 = 4
	switch {
	case score < 10:
		if strict {
			grade = 1
		}
	case score < 20:
		grade = 2
	default:
		grade = uint64(score)
	}
	return grade
}

func run() string {
	data := []byte{40, 50, 60}
	lines := ""
	for _, outer := range []bool{false, true} {
		for _, inner := range []bool{false, true} {
			lines += fmt.Sprintln(
				copied(outer, 10, 20), restored(outer, 3, 400), repeated(outer, 3, 400),
				nested(outer, inner), nestedUnsigned(outer, inner, 3, 4), nestedByte(data, outer, inner, 2),
				ladder(5, inner), ladder(15, inner), ladder(25, inner))
		}
	}
	return lines
}
