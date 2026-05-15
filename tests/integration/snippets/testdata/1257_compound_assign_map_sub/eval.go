package main

func run() int {
	m := map[string]int{"x": 100}
	m["x"] -= 30
	return m["x"]
}
