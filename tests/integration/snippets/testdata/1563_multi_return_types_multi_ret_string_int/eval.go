package main

func f() (string, int) { return "hello", 42 }

func run() string {
	s, _ := f()
	return s
}
