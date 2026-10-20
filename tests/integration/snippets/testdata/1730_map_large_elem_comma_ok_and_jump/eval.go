package main

import "fmt"

type wide [20]int64

func run() string {
	m := make(map[string]wide, 16)
	for i := range 16 {
		var w wide
		w[0], w[19] = int64(i+1), int64(-i-1)
		m[fmt.Sprint("k", i)] = w
	}
	hits, misses := int64(0), 0
	for i := range 40 {
		if w, ok := m[fmt.Sprint("k", i)]; ok {
			hits += w[0] - w[19]
		} else {
			misses++
		}
		if _, ok := m[fmt.Sprint("k", i)]; !ok {
			misses++
		}
	}
	w, ok := m["absent"]
	return fmt.Sprint(hits, misses, ok, w == wide{})
}
