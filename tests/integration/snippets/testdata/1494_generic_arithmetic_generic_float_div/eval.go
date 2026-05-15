package main

func div(a, b any) any {
	return a.(float64) / b.(float64)
}

func run() any {
	return div(5.0, 2.0)
}
