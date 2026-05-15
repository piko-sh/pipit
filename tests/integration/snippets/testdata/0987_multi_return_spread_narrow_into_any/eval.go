package main

import "fmt"

func pair() (int8, int32) {
	return 5, 300
}

func describe(first, second any) string {
	_, firstIsInt8 := first.(int8)
	_, secondIsInt32 := second.(int32)
	return fmt.Sprintf("%v %v int8=%v int32=%v", first, second, firstIsInt8, secondIsInt32)
}

func run() string {
	return describe(pair())
}
