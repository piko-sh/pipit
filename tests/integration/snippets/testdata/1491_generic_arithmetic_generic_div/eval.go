package main

func div(a, b any) any {
	return a.(int) / b.(int)
}

func run() any {
	return div(10, 2)
}
