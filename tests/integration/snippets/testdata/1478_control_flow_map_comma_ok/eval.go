package main

func run() int {
	m := map[string]int{"a": 42}
	v, ok := m["a"]
	result := 0
	if ok {
		result = v
	}
	return result
}
