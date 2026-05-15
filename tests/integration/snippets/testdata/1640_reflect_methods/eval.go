package main

import (
	"fmt"
	"reflect"
	"strings"
)

type T struct{ n int }

func (t T) Double() int    { return t.n * 2 }
func (t T) Add(x int) int  { return t.n + x }
func (t *T) Set(x int)     { t.n = x }
func (t T) String() string { return fmt.Sprint("T", t.n) }
func (t T) hidden() int    { return 0 }

type N int

func (n N) Inc() N { return n + 1 }

type foo struct{}

func (foo) X() string { return "x" }

var h = reflect.Type.MethodByName

func run() string {
	var lines []string
	rt := reflect.TypeOf(T{})
	lines = append(lines, fmt.Sprint(rt.NumMethod(), " ", reflect.TypeOf(&T{}).NumMethod(), " ", reflect.TypeOf(N(0)).NumMethod()))
	for i := range rt.NumMethod() {
		m := rt.Method(i)
		lines = append(lines, fmt.Sprint(i, " ", m.Name, " ", m.Type, " ", m.Index))
	}
	m, ok := rt.MethodByName("Add")
	lines = append(lines, fmt.Sprint(ok, " ", m.Name, " ", m.Func.Call([]reflect.Value{reflect.ValueOf(T{3}), reflect.ValueOf(4)})[0].Int()))
	v := reflect.ValueOf(T{5})
	lines = append(lines, fmt.Sprint(v.NumMethod(), " ", v.MethodByName("Double").Call(nil)[0].Int(), " ", v.Method(1).Call(nil)[0].Int(), " ", v.Method(1).Type()))
	_, ok = rt.MethodByName("Nope")
	lines = append(lines, fmt.Sprint(ok))
	lines = append(lines, fmt.Sprint(reflect.ValueOf(N(7)).MethodByName("Inc").Call(nil)[0].Int()))
	pm, ok := reflect.TypeOf(&T{}).MethodByName("Set")
	lines = append(lines, fmt.Sprint(ok, " ", pm.Type))
	fm, ok := h(reflect.TypeOf(foo{}), "X")
	lines = append(lines, fmt.Sprint(ok, " ", fm.Func.Interface().(func(foo) string)(foo{})))
	lines = append(lines, fmt.Sprint(reflect.TypeOf(T{}).Method(0).Func.Interface().(func(T, int) int)(T{1}, 2) == 3))
	func() {
		defer func() { lines = append(lines, fmt.Sprint("recovered: ", recover())) }()
		rt.Method(9)
	}()
	return strings.Join(lines, "\n")
}
