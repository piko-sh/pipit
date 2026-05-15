package main

type P struct{ v int }

func run() int {
	sum := 0
	f := func() {
		for i := range 1000 {
			p := &P{v: i}
			sum += p.v
		}
	}
	f()
	return sum
}
