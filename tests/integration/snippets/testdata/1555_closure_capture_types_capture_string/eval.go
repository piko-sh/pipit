package main

func make() func() string {
	x := "hello"
	return func() string { return x }
}

func run() string {
	f := make()
	return f()
}
