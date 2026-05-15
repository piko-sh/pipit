package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

type Box[T any] struct{ V T }

type Opt[T any] struct{ ok bool }

type Pair[K comparable, V any] struct {
	Key K
	Val V
}

type KV[K comparable, V any] struct {
	Key K
	Val V
}

func (p Pair[K, V]) String() string { return fmt.Sprintf("%v=%v", p.Key, p.Val) }

func sortAll[T int | string](vals ...T) []T {
	slices.Sort(vals)
	return vals
}

func describe[T any](v T) string { return fmt.Sprintf("%T=%v", v, v) }

func run() string {
	var lines []string
	a, b := Box[int]{1}, Box[string]{"x"}
	lines = append(lines, fmt.Sprintf("%T %T %v", a, b, reflect.TypeOf(a) == reflect.TypeOf(b)))
	lines = append(lines, reflect.TypeOf(a).String()+" "+reflect.TypeOf(b).Name())
	oi, os := Opt[int]{true}, Opt[string]{true}
	lines = append(lines, fmt.Sprintf("%T %T %v", oi, os, reflect.TypeOf(oi) == reflect.TypeOf(os)))
	var x any = oi
	_, isInt := x.(Opt[int])
	_, isStr := x.(Opt[string])
	lines = append(lines, fmt.Sprint(isInt, isStr))
	switch x.(type) {
	case Opt[string]:
		lines = append(lines, "string")
	case Opt[int]:
		lines = append(lines, "int")
	}
	lines = append(lines, fmt.Sprint(sortAll(3, 1, 2), sortAll("b", "a")))
	lines = append(lines, describe(Pair[string, int]{"k", 1})+" "+describe(42)+" "+describe([]Box[bool]{{true}}))
	out, _ := json.Marshal(KV[string, []int]{"k", []int{1, 2}})
	lines = append(lines, string(out))
	m := map[any]int{Opt[int]{true}: 1, Opt[string]{true}: 2}
	lines = append(lines, fmt.Sprint(len(m), m[Opt[int]{true}], m[Opt[string]{true}]))
	var st fmt.Stringer = Pair[int, bool]{7, true}
	lines = append(lines, st.String()+" "+fmt.Sprintf("%v %T", st, st))
	return strings.Join(lines, "\n")
}
