package main

func EntrypointRun() int {
	a := make([]int, 4)
	b := make([]int, 4)
	for i := 0; i < len(a); i++ {
		a[i] = i + 1
		b[i] = 4 - i
	}
	sum := 0
	for i := 0; i < len(a); i++ {
		sum += a[i] * b[i]
	}
	return sum
}
