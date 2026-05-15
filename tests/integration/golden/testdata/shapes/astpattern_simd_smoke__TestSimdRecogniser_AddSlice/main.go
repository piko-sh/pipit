package main

func EntrypointRun() float64 {
	a := make([]float64, 4)
	b := make([]float64, 4)
	destination := make([]float64, 4)
	for i := 0; i < len(a); i++ {
		a[i] = float64(i + 1)
		b[i] = float64((i + 1) * 10)
	}
	for i := 0; i < len(a); i++ {
		destination[i] = a[i] + b[i]
	}
	return destination[0] + destination[1] + destination[2] + destination[3]
}
