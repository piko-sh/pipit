package main

import "fmt"

var global = 3
var globalSmall uint16 = 9

type box struct {
	v     int
	inner struct{ v int }
}

type wrapper struct {
	box
	label string
}

func targets() string {
	var u uint = 2
	var small int8 = 1
	local := 5
	local <<= u
	local >>= 1
	global <<= small
	global <<= 2
	globalSmall >>= u
	p := new(int)
	*p = 6
	*p <<= u
	*p >>= 1
	captured := 3
	bump := func() { captured <<= u; captured >>= 1 }
	bump()
	arr := [3]uint8{1, 2, 3}
	arr[2] <<= small
	arr[1] <<= 3
	w := &wrapper{}
	w.inner.v = 1
	w.inner.v <<= u
	w.v <<= 2
	m := map[int]uint32{1: 8}
	m[1] >>= 2
	m[1] <<= small
	var signed int64 = -16
	signed >>= 2
	return fmt.Sprint(local, global, globalSmall, *p, captured, arr, w.v, m, signed)
}

type record struct {
	n     int
	inner struct{ m int }
}

func elements() string {
	var u uint = 3
	var small uint8 = 1
	s := []int{1, 2}
	s[1] <<= u
	s[0] <<= 4
	m := map[string]int{"a": 5}
	m["a"] >>= small
	m["a"] <<= 3
	r := &record{n: 7}
	r.n <<= small
	r.n >>= 1
	r.inner.m = 3
	r.inner.m <<= u
	return fmt.Sprint(s, m, r.n, r.inner.m)
}

type bytesOf []uint8

func throughPointers() string {
	var n uint = 3
	a := [4]int8{1, 2, 3, 4}
	p := &a
	i := 1
	p[i] <<= 2
	p[2] <<= 6
	p[3] <<= n
	q := &[2]int{5, 9}
	q[0] <<= n
	q[1] >>= 1
	named := bytesOf{200, 3}
	named[0] <<= 1
	named[1] <<= n
	return fmt.Sprint(a, *q, named)
}

func run() string {
	return targets() + "\n" + elements() + "\n" + throughPointers()
}
