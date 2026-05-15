package main

func make() func() float64 {
	x := 3.14
	return func() float64 { return x }
}

func run() float64 {
	f := make()
	return f()
}
