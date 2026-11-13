package main

import "fmt"

type pair struct{ a, b int }

func summed(p *pair, c bool, n int, q []int) int {
	s := 0
	if c {
		s = p.a
	}
	for {
		if n <= 0 || q[n] < 0 {
			break
		}
		s += p.b
		n--
	}
	return s
}

func caught(body func() int) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprint(r)
		}
	}()
	return fmt.Sprint(body())
}

func run() string {
	q := []int{0, 1, 2, 3}
	return fmt.Sprint(
		summed(&pair{1, 2}, true, 3, q), " ",
		summed(&pair{1, 2}, false, 3, q), " ",
		summed(nil, false, 0, q), " ",
		caught(func() int { return summed(nil, false, 2, q) }), " ",
		caught(func() int { return summed(nil, true, 0, q) }),
	)
}
