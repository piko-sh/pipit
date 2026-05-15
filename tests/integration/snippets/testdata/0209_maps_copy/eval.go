package main

import "maps"

func run() int {
	destination := map[string]int{"a": 1}
	source := map[string]int{"b": 2, "c": 3}
	maps.Copy(destination, source)
	return destination["a"] + destination["b"] + destination["c"]
}
