package main

import (
	"fmt"
	"strings"
)

var out []string

func note(args ...any) { out = append(out, fmt.Sprint(args...)) }

func g(prefix string, xs ...int) { note(prefix, " ", len(xs), " ", xs) }

func h(xs ...any) { note(len(xs), " ", xs) }

func total(base int, xs ...int) int {
	for _, x := range xs {
		base += x
	}
	return base
}

type logger struct{ prefix string }

func (l logger) logf(format string, args ...any) { note(l.prefix, fmt.Sprintf(format, args...)) }

func run() string {
	out = nil
	xs := []int{1, 2, 3}
	as := []any{"a", 2}
	func() {
		defer note("native packed ", 1, " two")
		defer note(as...)
		defer g("compiled packed", 1, 2, 3)
		defer g("compiled spread", xs...)
		defer g("compiled empty")
		defer h()
		defer h(as...)
		defer h(4, "five")
		defer note(fmt.Sprintf("%d", total(10, xs...)))
		defer note(fmt.Sprint(total(1)))
		l := logger{prefix: "L:"}
		defer l.logf("%s=%d", "k", 7)
		defer l.logf("plain")
		fn := g
		defer fn("via value", xs...)
		defer fn("via value packed", 5)
		note("body")
	}()
	xs[0] = 9
	as[0] = "changed"
	return strings.Join(out, "\n")
}
