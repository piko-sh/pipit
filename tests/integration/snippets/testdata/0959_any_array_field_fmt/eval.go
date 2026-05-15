package main

import "fmt"

type holder struct {
	mixed [2]any
}

func run() string {
	h := &holder{}
	h.mixed[0] = 42
	h.mixed[1] = "str"
	return fmt.Sprintf("%v %v", h.mixed[0], h.mixed[1])
}
