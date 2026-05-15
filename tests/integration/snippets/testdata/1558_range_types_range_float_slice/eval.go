package main

func run() float64 {
	sum := 0.0
	for _, v := range []float64{1.0, 2.0, 3.0} {
		sum += v
	}
	return sum
}
