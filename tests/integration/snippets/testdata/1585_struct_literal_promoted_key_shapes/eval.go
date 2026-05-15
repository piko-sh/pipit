package main

import "fmt"

type Object struct{ name, colour string }

type Line struct {
	Object
	length int
}

type Boxed[T any] struct {
	Object
	payload T
}

type Wrap struct{ L Line }

func run() string {
	pointer := &Line{name: "diag", length: 3}
	slice := []Line{{name: "a"}, {name: "b", colour: "red"}}
	array := [2]Line{{name: "x"}, {colour: "y"}}
	byKey := map[string]Line{"k": {name: "m", length: 1}}
	generic := Boxed[int]{name: "gen", payload: 42}
	nested := Wrap{L: Line{name: "inner", colour: "blue"}}

	return fmt.Sprintf("%s/%d|%s/%s|%s/%s|%s/%d|%s/%d|%s/%s",
		pointer.name, pointer.length,
		slice[0].name, slice[1].colour,
		array[0].name, array[1].colour,
		byKey["k"].name, byKey["k"].length,
		generic.name, generic.payload,
		nested.L.name, nested.L.Object.colour)
}
