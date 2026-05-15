package main

import "fmt"

func show[T any](v T) string { var zero T; return fmt.Sprintf("%T", zero) }

func applyTo[T any](f func(T) string, v T) string { return f(v) }

type inner struct{ fn func(float32) string }

type outer struct{ in inner }

func run() string {
	var assigned func(int8) string
	assigned = show

	array := [1]func(bool) string{show}

	channel := make(chan func(uint16) string, 1)
	channel <- show

	nested := outer{in: inner{fn: show}}

	return fmt.Sprintf("%s %s %s %s %s",
		assigned(1), array[0](true), (<-channel)(2),
		nested.in.fn(3), applyTo[int32](show, 4))
}
