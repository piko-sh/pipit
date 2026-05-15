package main

import "strings"

func run() int {
	return len(strings.Split("a,b,c", ","))
}
