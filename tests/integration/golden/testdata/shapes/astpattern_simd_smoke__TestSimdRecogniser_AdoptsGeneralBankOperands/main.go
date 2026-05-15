package main

func EntrypointRun() float64 {
	a := []float64{1, 2, 3, 4}
	b := []float64{4, 3, 2, 1}
	sum := 0.0
	for i := 0; i < len(a); i++ {
		sum += a[i] * b[i]
	}
	return sum
}
