package main

import "fmt"

type point struct{ x, y int }

var sinks [][]point

func keep(ps []point) { sinks = append(sinks, ps) }

func buildThenPass(n int) int {
	var ps []point
	for i := 0; i < n; i++ {
		ps = append(ps, point{x: i, y: i})
	}
	keep(ps)
	ps = append(ps, point{x: -1, y: -1})
	return len(ps)
}

func passEachIteration(n int) int {
	var ps []point
	for i := 0; i < n; i++ {
		ps = append(ps, point{x: i, y: i * 2})
		keep(ps)
	}
	return len(ps)
}

func passBefore(n int) int {
	ps := make([]point, 0, 16)
	keep(ps)
	for i := 0; i < n; i++ {
		ps = append(ps, point{x: i, y: 0})
	}
	return len(ps)
}

func run() string {
	lengths := []int{buildThenPass(5), passEachIteration(3), passBefore(4), buildThenPass(2)}
	var kept []int
	for _, sink := range sinks {
		kept = append(kept, len(sink))
	}
	return fmt.Sprint(lengths, kept, sinks[0], sinks[1])
}
