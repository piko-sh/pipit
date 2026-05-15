package main

import "fmt"

func run() string {
	var a int8 = -128
	var b int16 = -32768
	var c int32 = -2147483648
	return fmt.Sprintf("%d %d %d", -a, -b, -c)
}
