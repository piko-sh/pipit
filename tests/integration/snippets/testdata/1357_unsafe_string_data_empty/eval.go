package main

import (
	"unsafe"
)

func run() bool {
	s := ""
	p := unsafe.StringData(s)
	return p == nil
}
