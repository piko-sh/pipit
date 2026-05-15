package main

import (
	"fmt"
	"strings"
)

type ET struct{ code int }

func (e *ET) Error() string { return fmt.Sprint("et", e.code) }

func mayFail(fail bool) error {
	var e *ET
	if fail {
		e = &ET{7}
	}
	return e
}

type Counter struct {
	n    int
	tags []string
}

func (c Counter) Bump() int { c.n++; c.tags = append(c.tags, "x"); return c.n }

type Hasser interface{ M(int) int }
type NoArg struct{}

func (NoArg) M() int { return 1 }

type OneArg struct{}

func (OneArg) M(x int) int { return x + 1 }

type TwoOut struct{}

func (TwoOut) M(x int) (int, error) { return x, nil }

type S struct {
	n int
	f any
}

func run() string {
	var lines []string
	add := func(name string, f func() string) {
		defer func() {
			if r := recover(); r != nil {
				lines = append(lines, name+" panic: "+fmt.Sprint(r))
			}
		}()
		lines = append(lines, name+" "+f())
	}
	add("typed-nil", func() string {
		e := mayFail(false)
		var none error
		return fmt.Sprint(e == nil, e != nil, none == nil, e == none, none != e, mayFail(true) == nil)
	})
	add("iface-iface", func() string {
		a, b := mayFail(true), mayFail(true)
		c := a
		return fmt.Sprint(a == b, a == c, any(1) == any(int64(1)), any(1) == 1, any("s") == any("s"))
	})
	add("value-receiver", func() string {
		c := Counter{}
		a := c.Bump()
		b := c.Bump()
		p := &c
		d := p.Bump()
		cs := []Counter{{}, {}}
		e := cs[1].Bump()
		return fmt.Sprint(a, b, d, e, c.n, len(c.tags), cs[1].n)
	})
	add("signature-assert", func() string {
		vals := []any{NoArg{}, OneArg{}, TwoOut{}}
		var out []string
		for _, v := range vals {
			_, ok := v.(Hasser)
			out = append(out, fmt.Sprint(ok))
			switch v.(type) {
			case Hasser:
				out = append(out, "H")
			default:
				out = append(out, "-")
			}
		}
		return strings.Join(out, ",")
	})
	add("array-short-circuit", func() string {
		a := [2]any{1, []int{1}}
		b := [2]any{2, []int{1}}
		return fmt.Sprint(a == b, a != b)
	})
	add("struct-short-circuit", func() string {
		a := S{1, []int{1}}
		b := S{2, []int{1}}
		return fmt.Sprint(a == b, a != b)
	})
	add("struct-equal", func() string {
		a := S{1, "x"}
		b := S{1, "x"}
		return fmt.Sprint(a == b, [2]S{a, b} == [2]S{b, a})
	})
	add("array-uncomparable", func() string {
		a := [2]any{1, []int{1}}
		b := [2]any{1, []int{1}}
		return fmt.Sprint(a == b)
	})
	add("struct-uncomparable", func() string {
		a := S{1, map[string]int{}}
		b := S{1, map[string]int{}}
		return fmt.Sprint(a == b)
	})
	return strings.Join(lines, "\n")
}
