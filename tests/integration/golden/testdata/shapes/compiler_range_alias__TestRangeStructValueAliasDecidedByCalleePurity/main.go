package main

type body struct{ x, y float64 }

func scale(v float64) float64 { return v * 2 }
func sum(bodies []body) float64 {
	total := 0.0
	for _, b := range bodies {
		total += scale(b.x)
	}
	return total
}
func EntrypointRun() float64 { return sum([]body{{x: 1, y: 2}}) }
