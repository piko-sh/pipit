package main

import (
	"bufio"
	"fmt"
	"strings"
)

type speaker interface{ speak() int }

type base struct{ n int }

func (b *base) speak() int { return b.n }

type wrap struct {
	speaker
	depth int
}

type value struct{ n int }

func (v value) count() int { return v.n }

type holdsPointer struct{ *value }

func attempt(out *strings.Builder, name string, f func()) {
	defer func() { fmt.Fprintln(out, name, recover()) }()
	f()
}

func run() string {
	var out strings.Builder

	var s speaker = &base{n: 7}
	for depth := range 1000 {
		s = &wrap{speaker: s, depth: depth}
	}
	fmt.Fprintln(&out, "call", s.speak())
	bound := s.speak
	fmt.Fprintln(&out, "value through interface", bound())

	w := &wrap{speaker: &base{n: 3}}
	direct := w.speak
	fmt.Fprintln(&out, "value", direct(), w.speak())
	expression := (*wrap).speak
	fmt.Fprintln(&out, "expression", expression(w))
	var shallow speaker = wrap{speaker: &base{n: 4}}
	fmt.Fprintln(&out, "value receiver", shallow.speak())

	var written strings.Builder
	var flusher interface{ Flush() error } = bufio.NewReadWriter(nil, bufio.NewWriter(&written))
	if writer, ok := flusher.(*bufio.ReadWriter); ok {
		_, _ = writer.WriteString("native")
	}
	fmt.Fprintln(&out, "native embedding with a nil field", flusher.Flush(), written.String())

	attempt(&out, "nil embedded interface call", func() { (&wrap{}).speak() })
	attempt(&out, "nil embedded interface value", func() { empty := &wrap{}; f := empty.speak; f() })
	attempt(&out, "nil embedded interface through interface", func() { var e speaker = &wrap{}; e.speak() })
	attempt(&out, "nil embedded interface expression", func() { (*wrap).speak(&wrap{}) })
	attempt(&out, "nil receiver", func() { var none *wrap; none.speak() })
	attempt(&out, "nil receiver value", func() { var none *wrap; f := none.speak; f() })
	attempt(&out, "nil embedded pointer", func() { holdsPointer{}.count() })

	return out.String()
}
