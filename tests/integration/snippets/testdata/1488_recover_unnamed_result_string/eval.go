package main

func f() string {
	defer func() { recover() }()
	panic("boom")
}

func run() string {
	return f()
}
