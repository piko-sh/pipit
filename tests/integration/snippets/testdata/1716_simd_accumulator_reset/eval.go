package main

import "fmt"

func dense(weights [][]float64, input, bias, output []float64) {
	for i := 0; i < len(output); i++ {
		row := weights[i]
		sum := 0.0
		for j := 0; j < len(row); j++ {
			sum += row[j] * input[j]
		}
		sum += bias[i]
		if sum < 0 {
			sum = 0
		}
		output[i] = sum
	}
}

func totals(rows [][]float64) []float64 {
	out := make([]float64, len(rows))
	for i, row := range rows {
		total := 0.0
		for j := 0; j < len(row); j++ {
			total += row[j]
		}
		out[i] = total
	}
	return out
}

func run() string {
	weights := make([][]float64, 8)
	for i := range weights {
		weights[i] = make([]float64, 16)
		for j := range weights[i] {
			weights[i][j] = float64((i+j*7)%5) - 1.5
		}
	}
	input := make([]float64, 16)
	for j := range input {
		input[j] = float64(j%4) * 0.5
	}
	bias := []float64{1, -2, 0.5, 0, -1, 3, -0.5, 2}
	output := make([]float64, 8)
	dense(weights, input, bias, output)
	return fmt.Sprint(output, totals(weights))
}
