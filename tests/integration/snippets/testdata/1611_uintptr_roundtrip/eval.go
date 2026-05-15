package main

import (
	"fmt"
	"reflect"
	"unsafe"
)

type pair struct {
	a int64
	b int64
}

func run() string {
	p := pair{a: 1, b: 2}
	base := unsafe.Pointer(&p)
	bp := (*int64)(unsafe.Pointer(uintptr(base) + unsafe.Offsetof(p.b)))
	*bp = 20
	u := uintptr(base)
	arr := [3]int32{10, 20, 30}
	second := (*int32)(unsafe.Add(unsafe.Pointer(&arr[0]), 4))
	v := reflect.ValueOf(&p)
	flagField := (*uintptr)(unsafe.Pointer(uintptr(unsafe.Pointer(&v)) + unsafe.Sizeof(uintptr(0))*2))
	return fmt.Sprint(*bp, " ", p.b, " ", u != 0, " ", reflect.ValueOf(base).Pointer() == u, " ", *second, " ", *flagField != 0)
}
