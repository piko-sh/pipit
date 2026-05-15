package main

func EntrypointRun() float64 {
	a := make([]float64, 8)
	b := make([]float64, 8)
	for i := 0; i < len(a); i++ {
		a[i] = float64(i + 1)
		b[i] = float64(8 - i)
	}
	sum := 0.0
	for i := 0; i < len(a); i++ {
		sum += a[i] * b[i]
	}
	return sum
}
