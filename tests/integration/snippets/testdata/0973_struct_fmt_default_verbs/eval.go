package main

import "fmt"

type pair struct {
	x int
	y string
}

func run() string {
	v := pair{x: 1, y: "z"}
	return fmt.Sprintf("%v %+v", v, v)
}
