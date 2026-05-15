package main

import "fmt"

var events []string

func record() {
	events = append(events, "deferred")
}

func withTrivialDefer() int {
	defer record()
	events = append(events, "body")
	return len(events)
}

func voidTrivialDefer() {
	defer record()
	events = append(events, "vbody")
}

func run() string {
	total := 0
	for i := 0; i < 3; i++ {
		total += withTrivialDefer()
	}
	for i := 0; i < 2; i++ {
		voidTrivialDefer()
	}
	return fmt.Sprintf("%v %d", events, total)
}
