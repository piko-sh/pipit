package main

import (
	"fmt"
	"strings"
)

func compute(n int) int { return n * 3 }

func run() string {
	var lines []string
	x := compute(1)
	y := "a"
	{
		x := compute(2)
		y := "b"
		lines = append(lines, fmt.Sprint(x, y))
	}
	z := x + 10
	lines = append(lines, fmt.Sprint(x, y, z))

	p := &x
	{
		x := 100
		_ = x
	}
	q := 55
	lines = append(lines, fmt.Sprint(*p, q))

	s := 0
	for i := 0; i < 3; i++ {
		i := i * 10
		s += i
		t := s
		_ = t
	}
	lines = append(lines, fmt.Sprint(s))

	a := 5
	if a > 1 {
		a := a * 2
		b := a + 1
		lines = append(lines, fmt.Sprint(a, b))
	}
	c := a + 1
	lines = append(lines, fmt.Sprint(a, c))

	err := fmt.Errorf("outer")
	if err != nil {
		err := fmt.Errorf("inner")
		lines = append(lines, err.Error())
	}
	if err != nil {
		lines = append(lines, err.Error())
	}
	{
		err := fmt.Errorf("again")
		_ = err
	}
	w := 9
	lines = append(lines, fmt.Sprint(err, w))

	total := 0
	for _, n := range []int{1, 2, 3} {
		n := n
		defer func() { total += n }()
	}
	lines = append(lines, fmt.Sprint(total))
	return strings.Join(lines, "\n")
}
