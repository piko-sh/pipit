package main

import (
	"fmt"
	"sort"
	"strings"
)

type Status int

type Word string

func kinds(vals ...any) string {
	parts := make([]string, 0, len(vals))
	for _, v := range vals {
		parts = append(parts, fmt.Sprintf("%T", v))
	}
	return strings.Join(parts, " ")
}

func classify(v any) string {
	switch v.(type) {
	case int:
		return "int"
	case int32:
		return "int32"
	case int64:
		return "int64"
	case uint8:
		return "uint8"
	case float32:
		return "float32"
	case Status:
		return "Status"
	default:
		return "other"
	}
}

func run() string {
	var i int = 1
	var i8 int8 = 2
	var i32 int32 = 3
	var i64 int64 = 4
	var u uint = 5
	var u8 uint8 = 6
	var u32 uint32 = 7
	var u64 uint64 = 8
	var up uintptr = 9
	var f32 float32 = 1.5
	var f64 float64 = 2.5
	var c64 complex64 = 1 + 2i
	var s Status = 3
	var w Word = "w"
	ints := []int{1, 2}
	i64s := []int64{3}
	strs := []string{"a"}
	bytes := []byte("b")
	var a any = i32
	_, isInt := a.(int)
	v32, is32 := a.(int32)
	m := map[any]string{1: "int", int64(1): "int64", int32(1): "int32"}
	labels := make([]string, 0, len(m))
	for _, v := range m {
		labels = append(labels, v)
	}
	sort.Strings(labels)
	return kinds(i, i8, i32, i64, u, u8, u32, u64, up, f32, f64, c64, s, w, ints, i64s, strs, bytes, true, i > 0) + "|" +
		fmt.Sprint(isInt, is32, v32, len(m), labels) + "|" +
		fmt.Sprintf("%v %d %v", s, s, w) + "|" +
		classify(i) + classify(i32) + classify(i64) + classify(u8) + classify(f32) + classify(s) + classify(u)
}
