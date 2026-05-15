package main

import (
	"fmt"
	"strings"
)

func a() (r []int) {
	defer func() { r = append(r, 99) }()
	r = []int{1, 2}
	return r
}

func b() (r []int64) {
	defer func() { r = append(r, 99) }()
	r = []int64{1, 2}
	return r
}

func c() (r []int32) {
	defer func() { r = append(r, 99) }()
	r = []int32{1, 2}
	return r
}

func d() (r []int32) {
	r = []int32{1, 2}
	return r
}

func e() (r []int32) {
	defer func() { r[0] = 42 }()
	r = []int32{1, 2}
	return r
}

func f() (r []int32) {
	defer func() { r = append(r, 99) }()
	r = []int32{1, 2}
	return
}

func g() (n int32, r []uint16) {
	defer func() { r = append(r, 7); n++ }()
	return 1, []uint16{5}
}

func show(name string, fn func() any) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(&out, name, "panic:", r)
		}
	}()
	fmt.Fprintln(&out, name, fn())
}

var out strings.Builder

func run() string {
	show("a", func() any { return a() })
	show("b", func() any { return b() })
	show("c", func() any { return c() })
	show("d", func() any { return d() })
	show("e", func() any { return e() })
	show("f", func() any { return f() })
	show("g", func() any { n, r := g(); return fmt.Sprint(n, r) })
	return out.String()
}
