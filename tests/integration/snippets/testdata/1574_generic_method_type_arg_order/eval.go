package main

import (
	"fmt"
	"strings"
)

type S[A, B any] struct {
	a A
	b B
}

func (s S[A, B]) n[C, D any]() string {
	var c C
	var d D
	return typeStr(s.a, s.b, c, d)
}

func typeStr(args ...any) string {
	s := ""
	for i, argument := range args {
		if i > 0 {
			s += "->"
		}
		s += strings.TrimPrefix(fmt.Sprintf("%T", argument), "main.")
	}
	return s
}

type T1 int8
type T2 int16
type T3 int32
type T4 int64

func nCal[A, B, C, D any]() string { return S[A, B]{}.n[C, D]() }

func nVal[A, B, C, D any]() func() string { return S[A, B]{}.n[C, D] }

func nExp[A, B, C, D any]() func(S[A, B]) string { return S[A, B].n[C, D] }

func run() string {
	var got []string

	got = append(got, S[T1, T2]{}.n[T3, T4]())
	got = append(got, S[T4, T1]{}.n[T2, T3]())
	got = append(got, S[T3, T4]{}.n[T1, T2]())
	got = append(got, S[T2, T3]{}.n[T4, T1]())

	got = append(got, nCal[T1, T2, T3, T4]())
	got = append(got, nCal[T4, T1, T2, T3]())
	got = append(got, nCal[T3, T4, T1, T2]())
	got = append(got, nCal[T2, T3, T4, T1]())

	mv3 := S[T1, T2]{}.n[T3, T4]
	mv4 := S[T4, T1]{}.n[T2, T3]
	got = append(got, mv3(), mv4())
	got = append(got, nVal[T1, T2, T3, T4]()(), nVal[T4, T1, T2, T3]()())

	me3 := S[T1, T2].n[T3, T4]
	me4 := S[T4, T1].n[T2, T3]
	got = append(got, me3(S[T1, T2]{}), me4(S[T4, T1]{}))
	got = append(got, nExp[T1, T2, T3, T4]()(S[T1, T2]{}))
	got = append(got, nExp[T4, T1, T2, T3]()(S[T4, T1]{}))

	return strings.Join(got, " | ")
}
