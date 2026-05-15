package main

const N = 64

func EntrypointRun() float64 {
	a := make([]float64, N)
	for i := 0; i < len(a); i++ {
		a[i] = float64(i + 1)
	}
	sum := 0.0
	for i := 0; i < N; i++ {
		sum += a[i]
	}
	return sum
}
