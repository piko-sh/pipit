package main

import "fmt"

func run() string {
	var out string
	func() {
		defer func() {
			if v := recover(); v != nil {
				out = fmt.Sprintf("%d", v.(int))
			}
		}()
		panic(42)
	}()
	return out
}
