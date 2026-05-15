package main

func rem(a, b any) any {
	return a.(int) % b.(int)
}

func run() any {
	return rem(10, 3)
}
