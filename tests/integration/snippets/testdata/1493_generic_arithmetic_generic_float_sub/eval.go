package main

func sub(a, b any) any {
	return a.(float64) - b.(float64)
}

func run() any {
	return sub(5.5, 3.0)
}
