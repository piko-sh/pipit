package main

import "fmt"

type inner struct{ v int }

type outer struct {
	p *inner
	q any
}

func read(o *outer) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprint("recovered: ", r)
		}
	}()
	return fmt.Sprint(o.p.v)
}

func write(o *outer) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprint("recovered: ", r)
		}
	}()
	o.p.v = 3
	return "wrote"
}

func assertNil(o *outer) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = "recovered assertion"
		}
	}()
	return fmt.Sprint(o.q.(*inner).v)
}

func run() string {
	o := &outer{}
	a := read(o)
	b := write(o)
	c := assertNil(o)
	o.p = &inner{v: 7}
	o.q = &inner{v: 9}
	return a + "|" + b + "|" + c + "|" + read(o) + "|" + write(o) + "|" + assertNil(o) + "|" + fmt.Sprint(o.p.v)
}
