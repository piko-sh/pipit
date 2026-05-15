package main

import (
	"fmt"
	"reflect"
	"strings"
)

type Op func(int) int

func (o Op) Apply(x int) int { return o(x) }

type Applier interface{ Apply(int) int }

type S struct{ n int }

func (s S) Get() int { return s.n }

func kind(v any) string {
	switch f := v.(type) {
	case func() bool:
		return fmt.Sprint("bool-thunk ", f())
	case func(int) any:
		return fmt.Sprint("int-to-any ", f(1))
	case func(int) int:
		return fmt.Sprint("int-to-int ", f(2))
	case func():
		return "thunk"
	default:
		return fmt.Sprintf("other %T", v)
	}
}

func apply(fs ...func(string) string) string {
	out := "x"
	for _, f := range fs {
		out = f(out)
	}
	return out
}

func run() string {
	var lines []string
	var i any = func() bool { return true }
	f, ok := i.(func() bool)
	lines = append(lines, fmt.Sprint(ok, f != nil && f()))
	_, ok2 := i.(func(int) int)
	lines = append(lines, fmt.Sprint(ok2))
	lines = append(lines, kind(func() bool { return false })+"|"+kind(func(x int) any { return x })+"|"+kind(func(x int) int { return x * 2 })+"|"+kind(func() {})+"|"+kind(3))
	lines = append(lines, fmt.Sprintf("%T %T %T", i, func(a string, b ...int) (int, error) { return 0, nil }, S{}.Get))
	lines = append(lines, fmt.Sprint(reflect.TypeOf(func(int) string { return "" }), " ", reflect.TypeOf(i).Kind(), " ", reflect.TypeOf(i).NumIn(), " ", reflect.TypeOf(i).NumOut()))
	var a Applier = Op(func(x int) int { return x + 1 })
	lines = append(lines, fmt.Sprint(a.Apply(41), " ", fmt.Sprintf("%T", a)))
	g := S{5}.Get
	lines = append(lines, fmt.Sprint(g(), " ", reflect.TypeOf(g)))
	lines = append(lines, apply(strings.ToUpper, func(s string) string { return s + "!" }))
	var fn any = strings.TrimSpace
	_, isStrFn := fn.(func(string) string)
	lines = append(lines, fmt.Sprint(isStrFn))
	return strings.Join(lines, "\n")
}
