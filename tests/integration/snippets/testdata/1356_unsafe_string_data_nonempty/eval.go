package main

import (
	"unsafe"
)

func run() bool {
	s := "hello"
	p := unsafe.StringData(s)
	return p != nil
}
