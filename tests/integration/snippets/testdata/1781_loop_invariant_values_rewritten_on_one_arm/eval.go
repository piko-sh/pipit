package main

import "fmt"

func bump(t, i int) int {
	if i%2 == 0 {
		t++
	}
	return t
}

func scaleLate(t, i int) int {
	if i > 3 {
		t *= 3
	}
	return t
}

func productBumpedOnEvenSteps(a, b, n int) int {
	s := 0
	for i := 0; i < n; i++ {
		t := a * b
		if i%2 == 0 {
			t = t + 1
		}
		s += t
	}
	return s
}

func differenceTrimmedOnEvenSteps(a, b, n int) int {
	s := 0
	for i := 0; i < n; i++ {
		t := a - b
		if i&1 == 0 {
			t = t - 5
		}
		s += t * 3
	}
	return s
}

func inlinedBump(a, b, n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += bump(a*b, i)
	}
	return s
}

func inlinedScale(a, b, n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += scaleLate(a+b, i) + i
	}
	return s
}

func shiftMaskedOnSomeSteps(a, n int) int {
	s := 0
	for i := 0; i < n; i++ {
		t := a << 2
		if i&3 == 1 {
			t |= i
		}
		s ^= t
	}
	return s
}

func productAdjustedBySwitch(xs []int, a, b int) int {
	s := 0
	for _, x := range xs {
		t := a * b
		switch {
		case x > 2:
			t -= x
		case x < 0:
			t += x
		}
		s += t
	}
	return s
}

func narrowProductBumped(a, b uint32, n int) uint32 {
	var s uint32
	for i := 0; i < n; i++ {
		t := a * b
		if i%3 == 0 {
			t = t + uint32(i)
		}
		s += t
	}
	return s
}

func run() string {
	return fmt.Sprint(
		productBumpedOnEvenSteps(3, 4, 10),
		differenceTrimmedOnEvenSteps(9, 2, 10),
		inlinedBump(3, 4, 10),
		inlinedScale(9, 2, 10),
		shiftMaskedOnSomeSteps(5, 9),
		productAdjustedBySwitch([]int{1, 5, -2, 3}, 7, 3),
		narrowProductBumped(7, 9, 10),
		narrowProductBumped(1<<20, 1<<13, 7),
	)
}
