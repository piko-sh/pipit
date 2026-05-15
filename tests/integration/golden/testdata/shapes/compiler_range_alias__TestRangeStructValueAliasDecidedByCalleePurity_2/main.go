package main

type body struct{ x, y float64 }

var sink []body

func poke(v float64) float64 { sink[0].x = v; return v }
func sum(bodies []body) float64 {
	total := 0.0
	for _, b := range bodies {
		total += poke(b.x)
	}
	return total
}
func EntrypointRun() float64 { return sum([]body{{x: 1, y: 2}}) }
