package main

import "fmt"

type point struct{ x, y int }

func append(ps []point, p point) []point {
	grown := make([]point, len(ps)+1)
	copy(grown, ps)
	grown[len(ps)] = point{x: p.x * 10, y: p.y * 10}
	return grown
}

func grow() []point {
	var ps []point
	ps = append(ps, point{x: 1, y: 2})
	ps = append(ps, point{x: 3, y: 4})
	return ps
}

func growThenCopy() ([]point, []point) {
	var ps []point
	ps = append(ps, point{x: 1, y: 1})
	kept := ps
	ps = append(ps, point{x: 2, y: 2})
	return ps, kept
}

func run() string {
	ps, kept := growThenCopy()
	return fmt.Sprint(grow(), ps, kept)
}
