package main

func buildWeights(n int) [][]float64 {
	weights := make([][]float64, n)
	for i := 0; i < n; i++ {
		row := make([]float64, n)
		for j := 0; j < n; j++ {
			row[j] = float64(i*j) * 0.5
		}
		weights[i] = row
	}
	return weights
}
func EntrypointRun() int { return len(buildWeights(4)) }
