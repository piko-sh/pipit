package main

import "fmt"

type Box[A any] struct{}

func (b Box[A]) n[C any]() string {
	var a A
	var c C
	return fmt.Sprintf("%T %T", a, c)
}

func first() string {
	type T1 int8
	type T2 int16
	return Box[T1]{}.n[T2]()
}

func second() string {
	type T1 int32
	type T2 int64
	return Box[T1]{}.n[T2]()
}

func run() string { return first() + " | " + second() }
