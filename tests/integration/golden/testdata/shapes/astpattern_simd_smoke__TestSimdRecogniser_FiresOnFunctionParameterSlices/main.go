package main

func dotRow(row []float64, input []float64) float64 {
	sum := 0.0
	for j := 0; j < len(row); j++ {
		sum += row[j] * input[j]
	}
	return sum
}
func EntrypointRun() float64 {
	a := make([]float64, 8)
	b := make([]float64, 8)
	return dotRow(a, b)
}
