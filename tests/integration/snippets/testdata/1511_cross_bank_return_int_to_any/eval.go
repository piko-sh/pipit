package main

func f() any {
	return 42
}

func run() int {
	x := f().(int)
	return x
}
