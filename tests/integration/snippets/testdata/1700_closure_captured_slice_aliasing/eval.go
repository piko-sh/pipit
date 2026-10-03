package main

import (
	"fmt"
	"reflect"
)

func literalWrite() int64 {
	x := []int64{1, 2, 3}
	read := func() int64 { return x[0] }
	x[0] = 9
	return read()
}

func makeWrite() int64 {
	x := make([]int64, 3)
	read := func() int64 { return x[0] }
	x[0] = 9
	return read()
}

func aliasWrite() int64 {
	x := []int64{1, 2, 3}
	y := x
	read := func() int64 { return x[0] }
	y[0] = 9
	return read()
}

func nativeSwapLiteral() string {
	x := []string{"a", "b", "c"}
	read := func() string { return x[0] }
	reflect.Swapper(x)(0, 2)
	return read()
}

func nativeSwapMake() int64 {
	x := make([]int64, 3)
	x[0], x[2] = 1, 3
	read := func() int64 { return x[0] }
	reflect.Swapper(x)(0, 2)
	return read()
}

func closureWritesParentReads() int64 {
	x := []int64{1, 2, 3}
	write := func() { x[1] = 7 }
	write()
	return x[1]
}

func run() string {
	return fmt.Sprint(literalWrite(), makeWrite(), aliasWrite(), nativeSwapLiteral(), nativeSwapMake(),
		closureWritesParentReads())
}
