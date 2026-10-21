package main

import "fmt"

var k = 6

func run() string {
	var a uint8 = 3
	a = -a * uint8(k)
	var b uint16 = 3
	b = -b + uint16(k)
	var c uint32 = 3
	c = -c * 6
	var d uint64 = 3
	d = -d * uint64(k)
	var e uint = 3
	e = -e - uint(k)
	return fmt.Sprint(a, b, c, d, e)
}
