package main

func rowSum(n int) float64 {
	row := make([]float64, n)
	for j := 0; j < n; j++ {
		row[j] = float64(j)
	}
	sum := 0.0
	for j := 0; j < len(row); j++ {
		sum += row[j]
	}
	return sum
}
func EntrypointRun() float64 { return rowSum(8) }
