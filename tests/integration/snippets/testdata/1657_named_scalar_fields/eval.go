package main

import (
	"debug/elf"
	"fmt"
	"strings"
	"time"
)

type Status int

func (s Status) String() string { return [...]string{"off", "on"}[s] }

type Box struct {
	S Status
	N int
}

var table = [4]string{"none", "32", "64", "x"}

func classOf(h elf.FileHeader) string { return table[h.Class] }

var out strings.Builder

func run() string {
	h := elf.FileHeader{Class: elf.ELFCLASS64}
	fmt.Fprintln(&out, table[h.Class], classOf(h), h.Class, h.Class.String())
	fmt.Fprintf(&out, "%v %d %T\n", h.Class, h.Class, h.Class)
	var c elf.Class = elf.ELFCLASS32
	fmt.Fprintln(&out, table[c], c, time.Monday)
	fmt.Fprintln(&out, fmt.Sprint(c), fmt.Sprint(time.Monday))
	fmt.Fprintf(&out, "%v %s %d | %v %s %d\n", c, c, c, time.Monday, time.Monday, time.Monday)
	fmt.Fprintln(&out, []elf.Class{c}, map[elf.Class]int{c: 1})
	m := map[elf.Class]string{elf.ELFCLASS64: "sixtyfour"}
	fmt.Fprintln(&out, m[h.Class], m[c] == "")
	s := []string{"a", "b", "c"}
	fmt.Fprintln(&out, s[h.Class])
	switch h.Class {
	case elf.ELFCLASS64:
		fmt.Fprintln(&out, "is64")
	}
	var x any = h.Class
	_, ok := x.(elf.Class)
	fmt.Fprintln(&out, ok, x)
	b := Box{S: 1}
	fmt.Fprintln(&out, b.S, table[b.S], b.S.String())
	fmt.Fprintf(&out, "%v %d %T\n", b.S, b.S, b.S)
	var y any = b.S
	_, ok2 := y.(Status)
	fmt.Fprintln(&out, ok2, b)
	pb := &b
	pb.S = 0
	fmt.Fprintln(&out, pb.S, b, h)
	return out.String()
}
