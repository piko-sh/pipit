package main

func run() bool {
	var x any = 42
	_, ok := x.(int)
	return ok
}
