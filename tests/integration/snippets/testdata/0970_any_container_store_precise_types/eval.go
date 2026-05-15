package main

import "fmt"

type score int

type base struct {
	V any
}

type outer struct {
	base
	name string
}

type holder struct {
	mixed [2]any
	one   any
}

func run() string {
	h := &holder{}

	h.mixed[0] = score(3)
	_, namedOK := h.mixed[0].(score)

	h.mixed[0] = byte('x')
	_, byteOK := h.mixed[0].(byte)
	h.mixed[1] = 'r'
	_, runeOK := h.mixed[1].(rune)

	var a [2]any
	a[0] = 42
	_, localArrOK := a[0].(int)

	xs := make([]any, 1)
	xs[0] = 42
	_, sliceOK := xs[0].(int)

	h.one = 42
	_, fieldOK := h.one.(int)

	o := &outer{}
	o.V = 42
	o.name = "n"
	_, embOK := o.V.(int)

	h.one = float32(1.5)
	f32v, f32OK := h.one.(float32)

	h.mixed[0] = uint(9)
	_, uintOK := h.mixed[0].(uint)

	h.mixed[0] = true
	_, boolOK := h.mixed[0].(bool)
	h.mixed[1] = "s"
	_, strOK := h.mixed[1].(string)

	n := 3
	h.mixed[0] = n%2 == 1
	cbArr, cbArrOK := h.mixed[0].(bool)
	h.one = n > 2
	_, cbFieldOK := h.one.(bool)
	zs := make([]any, 1)
	zs[0] = n == 3
	_, cbSliceOK := zs[0].(bool)

	return fmt.Sprintf("named=%v byte=%v rune=%v localArr=%v slice=%v field=%v emb=%v f32=%v,%v uint=%v bool=%v str=%v cbArr=%v,%v cbField=%v cbSlice=%v %s",
		namedOK, byteOK, runeOK, localArrOK, sliceOK, fieldOK, embOK, f32v, f32OK, uintOK, boolOK, strOK, cbArr, cbArrOK, cbFieldOK, cbSliceOK, o.name)
}
