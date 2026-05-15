package main

func f() (int, string) { return 3, "hello" }

func run() string {
	_, s := f()
	return s
}
