package main

type body struct{ x, y float64 }

func sum(bodies []body) float64 {
	total := 0.0
	for _, b := range bodies {
		b.x = 5
		total += b.x
	}
	return total
}
func EntrypointRun() float64 {
	return sum([]body{{x: 1, y: 2}, {x: 3, y: 4}})
}
