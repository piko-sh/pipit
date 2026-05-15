package main

import (
	"fmt"
	"strings"
)

var trace []string

func note(s string) { trace = append(trace, s) }

func helper() {
	if recover() != nil {
		note("helper recovered")
	}
}

func viaHelper() { helper() }

func nestedHelper() {
	func() {
		if recover() != nil {
			note("nested recovered")
		}
	}()
}

func withResult() int { nestedHelper(); return 41 }

func tiny() { helper() }

func direct() {
	if r := recover(); r != nil {
		note(fmt.Sprint("direct recovered ", r))
	}
}

func mustPanic(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			note(name + " outer " + fmt.Sprint(r))
		} else {
			note(name + " outer nil")
		}
	}()
	f()
}

func run() string {
	trace = nil
	mustPanic("a", func() {
		defer viaHelper()
		panic("A")
	})
	mustPanic("b", func() {
		defer withResult()
		panic("B")
	})
	mustPanic("c", func() {
		defer tiny()
		panic("C")
	})
	mustPanic("d", func() {
		defer direct()
		panic("D")
	})
	mustPanic("e", func() {
		defer func() { helper() }()
		panic("E")
	})
	value := func() (n int) {
		defer withResult()
		defer func() int { return 99 }()
		n = 3
		return n
	}()
	note(fmt.Sprint("value ", value))
	return strings.Join(trace, "\n")
}
