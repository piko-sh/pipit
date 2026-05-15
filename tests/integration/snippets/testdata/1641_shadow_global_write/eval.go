package main

import (
	"fmt"
	"strings"
)

var g = 1
var h = "global"
var counter = 100

func write() string {
	g := 10
	g = 20
	h := "local"
	h += "!"
	counter++
	return fmt.Sprint(g, " ", h)
}

func swap() string {
	g, h := 5, "x"
	g, h = 6, "y"
	g++
	return fmt.Sprint(g, " ", h)
}

func pair() (int, string) { return 7, "z" }

func multi() string {
	g, h := pair()
	g += 1
	return fmt.Sprint(g, " ", h)
}

func closure() string {
	g := 1
	inc := func() { g++ }
	inc()
	inc()
	return fmt.Sprint(g)
}

func run() string {
	var lines []string
	lines = append(lines, write(), swap(), multi(), closure())
	lines = append(lines, fmt.Sprint(g, " ", h, " ", counter))
	{
		g := 99
		g *= 2
		lines = append(lines, fmt.Sprint(g))
	}
	x := 1
	{
		x := 2
		x++
		lines = append(lines, fmt.Sprint(x))
	}
	lines = append(lines, fmt.Sprint(x, " ", g))
	for i := 0; i < 2; i++ {
		i := i * 10
		i++
		lines = append(lines, fmt.Sprint(i))
	}
	g = 3
	h = "set"
	lines = append(lines, fmt.Sprint(g, " ", h))
	return strings.Join(lines, "\n")
}
