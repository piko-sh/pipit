package main

func sub(a, b any) any {
	return a.(int) - b.(int)
}

func run() any {
	return sub(10, 3)
}
