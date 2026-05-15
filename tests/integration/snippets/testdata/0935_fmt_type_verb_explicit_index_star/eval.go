package main

import "fmt"

func run() string {
	a := fmt.Sprintf("%[2]*d|%T", 5, 3, 42, "tail")
	b := fmt.Sprintf("%*d|%T", 4, 7, "mid")
	c := fmt.Sprintf("%[1]d %T", 9, int64(3))
	return a + " ## " + b + " ## " + c
}
