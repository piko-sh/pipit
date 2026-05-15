package main

import (
	"fmt"
	"strings"
)

func produce() []uint16 { return []uint16{7, 8} }

func ints() []int64 { return []int64{1} }

var out strings.Builder

func run() string {
	var a any = produce()
	fmt.Fprintf(&out, "%T %T %T\n", a, produce(), ints())
	r := produce()
	var b any = r
	fmt.Fprintf(&out, "%T\n", b)
	m := map[string]any{"p": produce()}
	fmt.Fprintf(&out, "%T\n", m["p"])
	s := []any{produce()}
	fmt.Fprintf(&out, "%T\n", s[0])
	m["q"] = produce()
	v, ok := m["q"].([]uint16)
	fmt.Fprintln(&out, ok, v)
	z := make([]uint16, 0)
	m["z"] = z
	zz, ok2 := m["z"].([]uint16)
	fmt.Fprintln(&out, ok2, zz == nil, len(zz))
	var empty []uint16
	m["e"] = empty
	e, ok3 := m["e"].([]uint16)
	fmt.Fprintln(&out, ok3, e == nil)
	m["b"] = []int8{-1}
	m["f"] = []float32{1.5}
	fmt.Fprintf(&out, "%T %T\n", m["b"], m["f"])
	fmt.Fprintln(&out, r, len(r), cap(produce()) >= 2)
	return out.String()
}
