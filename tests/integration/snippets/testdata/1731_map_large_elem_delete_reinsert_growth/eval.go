package main

import "fmt"

type wide struct {
	A, B, C, D string
	Vals       [12]int64
}

func run() string {
	var report []string
	for _, n := range []int{9, 64, 1024} {
		m := map[string]wide{}
		for i := range n {
			m[fmt.Sprint("k", i)] = wide{A: fmt.Sprint("a", i), Vals: [12]int64{int64(i)}}
		}
		for i := 0; i < n; i += 3 {
			delete(m, fmt.Sprint("k", i))
		}
		for i := 0; i < n; i += 6 {
			m[fmt.Sprint("k", i)] = wide{A: fmt.Sprint("b", i), Vals: [12]int64{int64(-i)}}
		}
		var sum int64
		count := 0
		for i := range n {
			if w, ok := m[fmt.Sprint("k", i)]; ok {
				sum += w.Vals[0]
				count++
				if w.A == "" {
					count += 1000
				}
			}
		}
		report = append(report, fmt.Sprint(n, len(m), count, sum))
	}
	return fmt.Sprint(report)
}
