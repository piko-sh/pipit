package main

import "fmt"

type point struct{ x, y int }

func seq(yield func(int) bool) {
	for i := 0; i < 2; i++ {
		if !yield(i) {
			return
		}
	}
}

func run() string {
	outer := point{x: 1, y: 2}
	var copies []int
	var funcs []func() int
	for i := range seq {
		rows := []point{{x: 3, y: 4}}
		row := rows[0]
		row.y = 40 + i
		local := point{x: 5, y: 6}
		copied := local
		copied.x = 50
		fromOuter := outer
		fromOuter.x = 10
		copies = append(copies, rows[0].y, row.y, local.x, copied.x, outer.x, fromOuter.x)
		for j := 0; j < 2; j++ {
			funcs = append(funcs, func() int { return i*10 + j })
		}
		for _, k := range []int{7, 8} {
			funcs = append(funcs, func() int { return k })
		}
	}
	var captured []int
	for _, f := range funcs {
		captured = append(captured, f())
	}
	return fmt.Sprint(copies, captured)
}
