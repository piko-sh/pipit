package main

func f() (int, string) { return 3, "hello" }

func run() int {
	x, _ := f()
	return x
}
