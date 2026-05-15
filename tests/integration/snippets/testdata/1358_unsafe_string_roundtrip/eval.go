package main

import (
	"unsafe"
)

func run() string {
	s := "hi"
	p := unsafe.StringData(s)
	result := unsafe.String(p, 2)
	return result
}
