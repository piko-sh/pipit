package main

import "fmt"

func run() string {
	var a int8 = -128
	var z8 int8 = 0
	subWrap := z8 - a

	var m int8 = 100
	m *= 2

	var s int16 = 30000
	s += 10000

	var n int8 = -5
	negOK := -n

	var mn int32 = -2147483648
	var one int32 = 1
	addWrap := mn - one

	return fmt.Sprintf("%d %d %d %d %d", subWrap, m, s, negOK, addWrap)
}
