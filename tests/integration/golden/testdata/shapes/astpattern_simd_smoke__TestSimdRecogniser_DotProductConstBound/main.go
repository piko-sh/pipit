package main

const N = 1024

func EntrypointRun() float64 {
	a := make([]float64, N)
	b := make([]float64, N)
	for i := 0; i < len(a); i++ {
		a[i] = float64(i + 1)
		b[i] = float64(N - i)
	}
	sum := 0.0
	for i := 0; i < N; i++ {
		sum += a[i] * b[i]
	}
	return sum
}
