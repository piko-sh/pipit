package main

import (
	"errors"
	"fmt"
	"strings"
)

type Celsius float32
type Meters int
type Label string

func describe(format string, args ...any) string { return fmt.Sprintf(format, args...) }

func run() string {
	var lines []string
	c := Celsius(36.6) * 2
	m := Meters(3) * 2
	l := Label("x") + "y"
	var f32 float32 = 0.1
	var i8 int8 = -3

	lines = append(lines, fmt.Sprintf("%T %T %T %T %T", c, m, l, f32, i8))
	lines = append(lines, fmt.Sprintf("%v %v %v %v %v", c, m, l, f32*3, i8))
	lines = append(lines, describe("%T|%T|%v", c, any(m), f32))

	var builder strings.Builder
	fmt.Fprintf(&builder, "%T=%v %T=%v", c, c, f32, f32)
	lines = append(lines, builder.String())

	err := fmt.Errorf("%T wraps %w", m, errors.New("inner"))
	lines = append(lines, err.Error())

	values := []any{c, m, f32, 1 < 2, i8}
	for _, v := range values {
		lines = append(lines, fmt.Sprintf("%T", v))
	}
	lines = append(lines, fmt.Sprint(values...))
	return strings.Join(lines, "\n")
}
