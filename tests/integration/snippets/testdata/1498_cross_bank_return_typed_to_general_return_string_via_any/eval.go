package main

func getString() string { return "hi" }

func wrap() any { return getString() }

func run() string {
	x := wrap()
	y := x.(string)
	return y
}
