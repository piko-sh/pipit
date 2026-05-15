package main

type body struct{ x, y float64 }

func sum(bodies []body) float64 {
	total := 0.0
	for _, b := range bodies {
		bodies[1].x = 100
		total += b.x
	}
	return total
}
func EntrypointRun() float64 {
	return sum([]body{{x: 1, y: 2}, {x: 3, y: 4}})
}
