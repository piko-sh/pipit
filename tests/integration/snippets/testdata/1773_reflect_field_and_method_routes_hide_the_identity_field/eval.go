package main

import (
	"fmt"
	"reflect"
	"strings"
)

type Base struct {
	ID   int
	Name string
}

func (Item) Describe() string { return "item" }

type Item struct {
	Base
	Price float64
	tags  []string
}

func (i Item) Total() float64 { return i.Price }

type Wrapper struct {
	*Base
	Item  Item
	Count int
}

func names[T any](seq func(func(T) bool), name func(T) string) []string {
	out := []string{}
	for v := range seq {
		out = append(out, name(v))
	}
	return out
}

var out strings.Builder

func run() string {
	t := reflect.TypeFor[Item]()
	fmt.Fprintln(&out, names(t.Fields(), func(f reflect.StructField) string { return fmt.Sprint(f.Name, f.Anonymous, f.Index) }))
	fmt.Fprintln(&out, names(t.Methods(), func(m reflect.Method) string { return m.Name }))
	visible := []string{}
	for _, f := range reflect.VisibleFields(t) {
		visible = append(visible, fmt.Sprint(f.Name, f.Index))
	}
	fmt.Fprintln(&out, visible)
	wrapperVisible := []string{}
	for _, f := range reflect.VisibleFields(reflect.TypeOf(Wrapper{})) {
		wrapperVisible = append(wrapperVisible, fmt.Sprint(f.Name, f.Index))
	}
	fmt.Fprintln(&out, wrapperVisible)
	fmt.Fprintln(&out, t.FieldByIndex([]int{0, 1}).Name, t.FieldByIndex([]int{1}).Name)
	func() {
		defer func() { fmt.Fprintln(&out, "recovered:", recover()) }()
		fmt.Fprintln(&out, t.FieldByIndex([]int{3}).Name)
	}()
	v := reflect.ValueOf(Item{Base: Base{ID: 7, Name: "n"}, Price: 2.5})
	values := []string{}
	for f, fv := range v.Fields() {
		if f.IsExported() {
			values = append(values, fmt.Sprint(f.Name, "=", fv.Interface()))
		} else {
			values = append(values, f.Name+"(unexported)")
		}
	}
	fmt.Fprintln(&out, values)
	methods := []string{}
	for m, mv := range v.Methods() {
		methods = append(methods, fmt.Sprint(m.Name, mv.Call(nil)[0].Interface()))
	}
	fmt.Fprintln(&out, methods)
	fmt.Fprintln(&out, v.FieldByIndex([]int{0, 0}).Int(), v.FieldByIndex([]int{1}).Float())
	fieldsOf := v.Fields
	count := 0
	for range fieldsOf() {
		count++
	}
	typeFieldsOf := t.Fields
	typeCount := 0
	for range typeFieldsOf() {
		typeCount++
	}
	fmt.Fprintln(&out, count, typeCount, t.NumField(), v.NumField())
	return out.String()
}
