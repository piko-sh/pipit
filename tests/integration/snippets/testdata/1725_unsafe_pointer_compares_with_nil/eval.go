package main

import (
	"fmt"
	"unsafe"
)

func run() string {
	var p unsafe.Pointer
	x := 1
	q := unsafe.Pointer(&x)
	var r unsafe.Pointer = nil
	return fmt.Sprint(p == nil, q == nil, nil != p, q != nil, r == p)
}
