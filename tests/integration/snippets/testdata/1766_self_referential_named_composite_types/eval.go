package main

import (
	"fmt"
	"reflect"
	"strings"
)

type L []L
type L2 []L2
type F func() F
type F2 func() F2
type M map[string]M
type M2 map[string]M2
type A [2]*A
type C chan C

type X []Y
type Y []X

type LS []LS

func (l LS) String() string { return fmt.Sprintf("LS%d", len(l)) }

var out strings.Builder

func check(label string, f func() string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(&out, label, "PANIC", r)
		}
	}()
	fmt.Fprintln(&out, label, f())
}

func run() string {
	check("assert-L", func() string {
		var x any = L{nil}
		_, ok := x.(L2)
		_, ok2 := x.(L)
		return fmt.Sprint(ok, ok2)
	})
	check("T", func() string {
		return fmt.Sprintf("%T %T %T %T %T %T", L{}, F(nil), M{}, A{}, C(nil), X{})
	})
	check("assert-M", func() string {
		var x any = M{}
		_, ok := x.(M2)
		return fmt.Sprint(ok)
	})
	check("assert-F", func() string {
		var f F
		f = func() F { return f }
		var x any = f
		_, ok := x.(F2)
		_, ok2 := x.(F)
		return fmt.Sprint(ok, ok2)
	})
	check("switch", func() string {
		var x any = L2{L2{}}
		switch v := x.(type) {
		case L:
			return "L"
		case L2:
			return fmt.Sprint("L2 ", len(v), len(v[0]))
		}
		return "none"
	})
	check("eq", func() string {
		var a any = M(nil)
		var b any = M2(nil)
		return fmt.Sprint(a == b)
	})
	check("elem", func() string {
		t := reflect.TypeOf(L{})
		return fmt.Sprint(t, " ", t.Elem(), " ", t.Elem() == t, " ", reflect.TypeOf(X{}).Elem(), " ", reflect.TypeOf(X{}).Elem().Elem() == reflect.TypeOf(X{}))
	})
	check("nest", func() string {
		l := L{L{L{}}, L{}}
		return fmt.Sprint(len(l), len(l[0]), len(l[0][0]))
	})
	check("stringer", func() string {
		return fmt.Sprint(LS{LS{}, LS{}}, " ", LS{}[:])
	})
	check("mutual", func() string {
		x := X{Y{X{}}}
		var a any = x[0]
		_, ok := a.(Y)
		_, ok2 := a.(X)
		return fmt.Sprint(ok, ok2, len(x[0][0]))
	})
	check("chan", func() string {
		c := make(C, 1)
		c <- c
		d := <-c
		var a any = d
		_, ok := a.(C)
		return fmt.Sprint(ok, d == c)
	})
	check("array", func() string {
		var a A
		a[0] = &a
		var x any = *a[0]
		_, ok := x.(A)
		return fmt.Sprint(ok, a[0][0] == &a)
	})
	check("map", func() string {
		m := M{"a": M{"b": nil}}
		return fmt.Sprint(len(m), len(m["a"]), m["a"]["b"] == nil)
	})
	return out.String()
}
