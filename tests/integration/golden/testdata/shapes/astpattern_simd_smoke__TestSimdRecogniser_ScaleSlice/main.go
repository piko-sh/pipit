package main

func EntrypointRun() float64 {
	a := make([]float64, 4)
	for i := 0; i < len(a); i++ {
		a[i] = float64(i + 1)
	}
	k := 2.5
	for i := 0; i < len(a); i++ {
		a[i] *= k
	}
	return a[0] + a[1] + a[2] + a[3]
}
