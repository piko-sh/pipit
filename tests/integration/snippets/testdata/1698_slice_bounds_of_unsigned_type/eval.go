package main

import "fmt"

func run() string {
	ints := []int{1, 2, 3, 4, 5}
	names := []string{"a", "b", "c", "d"}
	boxed := []any{1, "two", 3.0, nil}
	var high uint = 3
	var low uint8 = 1
	var limit uint16 = 4
	return fmt.Sprint(ints[low:high], len(ints[:high]), names[low:high:limit], cap(names[low:high:limit]),
		boxed[low:high], len(boxed[:high]))
}
