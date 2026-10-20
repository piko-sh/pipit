package main

import "fmt"

var limit = 3

func seq(yield func(int) bool) {
	if !yield(5) {
		return
	}
	yield(7)
}

func outerWrites() int {
	v := 0
	for k := 0; k < limit; k++ {
		v++
		for x := range seq {
			v += x
		}
	}
	return v
}

func declaredInTheLoop() int {
	total := 0
	for k := 0; k < limit; k++ {
		w := 0
		for x := range seq {
			w += x
		}
		total += w
	}
	return total
}

func breakEachTime() int {
	count := 0
	for k := 0; k < limit; k++ {
		for x := range seq {
			count += x
			break
		}
	}
	return count
}

func run() string {
	return fmt.Sprint(outerWrites(), declaredInTheLoop(), breakEachTime())
}
