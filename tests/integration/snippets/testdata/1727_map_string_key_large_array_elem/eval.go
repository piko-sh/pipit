package main

import "fmt"

type wide [17]int64

func run() string {
	m := map[string]wide{}
	for i := range 1000 {
		var w wide
		for j := range w {
			w[j] = int64(i*100 + j)
		}
		m[fmt.Sprintf("key-%d", i)] = w
	}
	var sum int64
	for i := range 1000 {
		w := m[fmt.Sprintf("key-%d", i)]
		sum += w[0] + w[16]
	}
	return fmt.Sprintf("len=%d sum=%d", len(m), sum)
}
