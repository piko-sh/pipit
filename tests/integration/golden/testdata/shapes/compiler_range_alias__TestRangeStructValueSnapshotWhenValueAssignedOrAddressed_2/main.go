package main

type body struct{ x, y float64 }

func sum(bodies []body) float64 {
	total := 0.0
	for _, b := range bodies {
		p := &b
		total += p.x
	}
	return total
}
func EntrypointRun() float64 {
	return sum([]body{{x: 1, y: 2}, {x: 3, y: 4}})
}
