package main

func run() string {
	s := "hello"
	p := &s
	return *p
}
