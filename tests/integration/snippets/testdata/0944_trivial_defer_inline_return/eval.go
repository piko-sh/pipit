package main

import "fmt"

var order []string

func addValue() int {
	defer func() { order = append(order, "deferred") }()
	order = append(order, "body")
	return 1
}

func addVoid() {
	defer func() { order = append(order, "vdeferred") }()
	order = append(order, "vbody")
}

func run() string {
	total := 0
	for i := 0; i < 3; i++ {
		total += addValue()
	}
	for i := 0; i < 2; i++ {
		addVoid()
	}
	return fmt.Sprintf("%v %d", order, total)
}
