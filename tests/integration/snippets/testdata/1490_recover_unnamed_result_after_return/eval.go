package main

func f() int {
	defer func() { recover() }()
	defer func() { panic("late") }()
	return 42
}

func run() int {
	return f()
}
