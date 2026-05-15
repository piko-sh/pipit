package main

type point struct {
	x int
	y int
}

var sink []point

func keep(ps []point) { sink = ps }

func build(n int) int {
	ps := []point{}
	for i := 0; i < n; i++ {
		ps = append(ps, point{x: i, y: i})
	}
	return len(ps)
}

func leak(n int) int {
	ps := []point{}
	for i := 0; i < n; i++ {
		ps = append(ps, point{x: i, y: i})
	}
	keep(ps)
	return len(ps)
}

func ret(n int) []point {
	ps := []point{}
	for i := 0; i < n; i++ {
		ps = append(ps, point{x: i, y: i})
	}
	return ps
}

func EntrypointRun() int { return build(3) + leak(3) + len(ret(3)) }
