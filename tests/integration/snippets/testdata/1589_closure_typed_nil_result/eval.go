package main

type T struct{ v int }

func run() bool {
	f := func() *T { return nil }
	var i any = f()
	return i == nil
}
