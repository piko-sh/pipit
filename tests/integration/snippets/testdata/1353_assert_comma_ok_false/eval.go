package main

func run() bool {
	var x any = "hello"
	_, ok := x.(int)
	return ok
}
