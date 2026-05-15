package main

func run() float64 {
	sum := 0.0
	for _, v := range []float64{1.1, 2.2, 3.3} {
		sum += v
	}
	return sum
}
