package main

import (
	"errors"
	"fmt"
)

var errDone = errors.New("done")

func zero() (_ string, x float64, err error) {
	return
}

func explicit() (_ string, x float64, err error) {
	return "hello", 3.14, errDone
}

func deferred() (_ int, n int) {
	defer func() { n++ }()
	return 7, 1
}

func run() string {
	a, b, c := zero()
	d, e, f := explicit()
	g, h := deferred()
	return fmt.Sprint(a == "", " ", b, " ", c == nil, " ", d, " ", e, " ", f, " ", g, " ", h)
}
