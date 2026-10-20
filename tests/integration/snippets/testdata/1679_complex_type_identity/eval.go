package main

import (
	"fmt"
	"reflect"
	"strings"
)

func typeFor[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}

var numericTypes = []reflect.Type{
	typeFor[float32](),
	typeFor[float64](),
	typeFor[complex64](),
	typeFor[complex128](),
	typeFor[[]complex64](),
	typeFor[map[string]complex128](),
}

func describe(value any) string {
	return fmt.Sprintf("%T=%v", value, value)
}

func run() string {
	var out strings.Builder
	for _, t := range numericTypes {
		out.WriteString(t.String())
		out.WriteString(" ")
	}
	out.WriteString("| ")
	var c64 complex64 = complex(1, 2)
	c128 := complex(3.5, -4)
	out.WriteString(describe(c64) + " " + describe(c128) + " " + describe(real(c128)) + " " + describe(imag(c64)))
	out.WriteString(fmt.Sprintf(" | %v %v", reflect.TypeOf(c64) == typeFor[complex64](), reflect.TypeOf(c128) == typeFor[complex128]()))
	out.WriteString(fmt.Sprintf(" | %v %v %d %d", reflect.TypeOf(c64).Kind(), reflect.TypeOf(c128).Kind(), typeFor[complex64]().Size(), typeFor[complex128]().Size()))
	return out.String()
}
