package main

import (
	"fmt"
	"strings"
)

func show[T any](v T) string {
	var zero T
	return fmt.Sprintf("%T", zero)
}

func apply(f func(int8) string, v int8) string { return f(v) }

type Handler func(string) string

func returned() func(uint16) string { return show }

type holder struct{ fn func(rune) string }

func run() string {
	var assigned func(int8) string = show
	var named Handler = show
	elements := []func(bool) string{show}
	field := holder{fn: show}

	return strings.Join([]string{
		assigned(1),
		apply(show, 2),
		named("x"),
		elements[0](true),
		returned()(9),
		field.fn('a'),
	}, " ")
}
