package main

import "fmt"

type Plain struct{ n int }

func (p Plain) conv[P any](v P) string { var z P; return fmt.Sprintf("%d/%T/%T", p.n, v, z) }

func (p Plain) two[P any](v P) (P, int) { return v, p.n }

func (p Plain) sum[P ~int | ~int64](vs ...P) P {
	var total P
	for _, v := range vs {
		total += v
	}
	return total
}

type Outer struct{ Plain }

type Counter struct{ n int }

func (c Counter) down[P any](k int, tag P) string {
	if k <= 0 {
		return fmt.Sprintf("%T", tag)
	}
	return "." + c.down[P](k-1, tag)
}

func run() string {
	p := Plain{n: 7}
	first, second := p.two[string]("x")
	return fmt.Sprintf("%s|%s %d|%d|%s|%s",
		p.conv[int16](3),
		first, second,
		p.sum[int64](1, 2, 3),
		Outer{Plain: Plain{n: 9}}.conv[bool](true),
		Counter{n: 1}.down[uint8](3, 0))
}
