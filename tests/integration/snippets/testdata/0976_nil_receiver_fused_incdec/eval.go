package main

import "fmt"

type box struct {
	n int
}

func incThroughNil() (out string) {
	var p *box
	defer func() {
		out = fmt.Sprintf("inc: %v", recover())
	}()
	p.n++
	return "inc: no panic"
}

func run() string {
	return incThroughNil()
}
