package main

func run() int {
	m := map[string]int{"a": 1}
	m["a"] += 5
	return m["a"]
}
