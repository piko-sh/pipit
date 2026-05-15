package main

import (
	"fmt"
	"reflect"
)

type Word string

func run() string {
	var w any = Word("w")

	out := fmt.Sprintf("T=%T;", w)
	out += fmt.Sprintf("eqPlain=%v;eqNamed=%v;", w == "w", w == Word("w"))

	func() {
		defer func() {
			if r := recover(); r != nil {
				out += fmt.Sprintf("panic=%v;", r)
			}
		}()
		_ = w.(string)
	}()

	rt := reflect.TypeOf(w)
	out += fmt.Sprintf("rt=%s,kind=%s,name=%s", rt.String(), rt.Kind(), rt.Name())
	return out
}
