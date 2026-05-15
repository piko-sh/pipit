package main

func f() (x int) {
	defer func() { x += 10 }()
	x = 0
	return
}

func run() int {
	return f()
}
