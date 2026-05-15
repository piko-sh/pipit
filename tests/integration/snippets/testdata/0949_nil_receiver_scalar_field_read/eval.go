package main

import "fmt"

type mixed struct {
	i   int
	f64 float64
}

func readThroughNil() (out string) {
	var p *mixed
	touched := false
	defer func() {
		r := recover()
		touched = r != nil
		out = fmt.Sprintf("nil-deref recovered: %v (%v)", touched, r)
	}()
	leaked := p.i
	return fmt.Sprintf("unreachable %d", leaked)
}

func run() string {
	return readThroughNil()
}
