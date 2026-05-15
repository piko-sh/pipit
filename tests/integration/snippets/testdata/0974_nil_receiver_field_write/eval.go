package main

import "fmt"

type box struct {
	n     int
	label string
}

func writeIntThroughNil() (out string) {
	var p *box
	defer func() {
		out = fmt.Sprintf("int write: %v", recover())
	}()
	p.n = 42
	return "int write: no panic"
}

func writeStringThroughNil() (out string) {
	var p *box
	defer func() {
		out = fmt.Sprintf("string write: %v", recover())
	}()
	p.label = "x"
	return "string write: no panic"
}

func run() string {
	return writeIntThroughNil() + "\n" + writeStringThroughNil()
}
