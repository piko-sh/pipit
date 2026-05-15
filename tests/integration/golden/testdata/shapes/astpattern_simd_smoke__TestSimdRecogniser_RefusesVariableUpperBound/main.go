package main

func EntrypointRun() float64 {
	a := make([]float64, 5)
	for i := 0; i < len(a); i++ {
		a[i] = float64(i + 1)
	}
	n := 3
	sum := 0.0
	for i := 0; i < n; i++ {
		sum += a[i]
	}
	return sum
}
