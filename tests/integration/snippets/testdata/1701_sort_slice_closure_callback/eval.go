package main

import (
	"fmt"
	"reflect"
	"sort"
)

const length = 257

func scrambled() []int64 {
	x := make([]int64, length)
	for i := range length {
		x[i] = int64(i) * 27644437 % int64(length)
	}
	return x
}

func isSorted(x []int64) bool {
	for i := 0; i < len(x)-1; i++ {
		if x[i] >= x[i+1] {
			return false
		}
	}
	return true
}

func sortSlice() bool {
	x := scrambled()
	sort.Slice(x, func(i, j int) bool { return x[i] < x[j] })
	return isSorted(x)
}

func sortSliceStableReflect() bool {
	x := scrambled()
	static := func(i, j int) bool { return x[i] < x[j] }
	less := reflect.MakeFunc(reflect.TypeOf(static), func(args []reflect.Value) []reflect.Value {
		return []reflect.Value{reflect.ValueOf(x[args[0].Int()] < x[args[1].Int()])}
	}).Interface().(func(i, j int) bool)
	sort.SliceStable(x, less)
	return isSorted(x)
}

func callbackSeesSwaps() string {
	x := []int64{3, 1, 2}
	var seen []int64
	sort.Slice(x, func(i, j int) bool {
		seen = append(seen, x[0])
		return x[i] < x[j]
	})
	return fmt.Sprint(seen, x)
}

func run() string {
	return fmt.Sprint(sortSlice(), sortSliceStableReflect(), callbackSeesSwaps())
}
