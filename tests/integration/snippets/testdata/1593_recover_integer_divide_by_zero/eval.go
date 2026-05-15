package main

import "fmt"

func safeDiv(a, b int) (result int, message string) {
	defer func() {
		if r := recover(); r != nil {
			message = fmt.Sprint(r)
		}
	}()
	return a / b, ""
}

func safeMod(a, b int) (result int, message string) {
	defer func() {
		if r := recover(); r != nil {
			message = fmt.Sprint(r)
		}
	}()
	return a % b, ""
}

func loopDiv(values []int) (sum int, message string) {
	defer func() {
		if r := recover(); r != nil {
			message = fmt.Sprint(r)
		}
	}()
	for _, v := range values {
		sum += 100 / v
	}
	return sum, ""
}

func run() string {
	_, m1 := safeDiv(7, 0)
	q, m2 := safeDiv(7, 2)
	_, m3 := safeMod(7, 0)
	s, m4 := loopDiv([]int{5, 4, 0, 2})
	return fmt.Sprintf("%s|%d %q|%s|%d %s", m1, q, m2, m3, s, m4)
}
