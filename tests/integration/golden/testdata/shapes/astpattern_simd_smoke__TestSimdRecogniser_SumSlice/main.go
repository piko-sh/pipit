package main

func EntrypointRun() float64 {
	a := make([]float64, 4)
	a[0] = 1.5
	a[1] = 2.5
	a[2] = 3.5
	a[3] = 4.5
	sum := 0.0
	for i := 0; i < len(a); i++ {
		sum += a[i]
	}
	return sum
}
