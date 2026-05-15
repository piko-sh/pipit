package main

func f() int {
	defer func() { recover() }()
	panic("boom")
}

func run() int {
	return f()
}
