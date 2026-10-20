package main

import (
	"fmt"
	"net"
	"os"
	"sort"
)

func names(n int) int {
	var s sort.StringSlice
	for range n {
		s = append(s, "x")
	}
	return len(s)
}

func ints(n int) int {
	var s sort.IntSlice
	for i := range n {
		s = append(s, i)
	}
	return len(s)
}

func address(n int) int {
	var b net.IP
	for range n {
		b = append(b, 1)
	}
	return len(b)
}

func growArgs() int {
	a := os.Args
	a = append(a, "extra")
	return len(a) - len(os.Args)
}

func run() string {
	before := len(os.Args)
	grown := []int{growArgs(), growArgs()}
	return fmt.Sprint(names(3), names(3), names(2), ints(3), ints(3), ints(2),
		address(3), address(3), address(2), grown, len(os.Args)-before)
}
