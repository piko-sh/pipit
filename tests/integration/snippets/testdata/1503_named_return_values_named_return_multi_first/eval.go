package main

func f() (a int, b string) { a = 1; b = "x"; return }

func run() int {
	a, _ := f()
	return a
}
