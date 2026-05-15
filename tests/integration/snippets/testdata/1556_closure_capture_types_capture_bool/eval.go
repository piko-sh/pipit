package main

func make() func() bool {
	x := true
	return func() bool { return x }
}

func run() bool {
	f := make()
	return f()
}
