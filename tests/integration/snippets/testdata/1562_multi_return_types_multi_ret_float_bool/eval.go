package main

func f() (float64, bool) { return 3.14, true }

func run() float64 {
	v, _ := f()
	return v
}
