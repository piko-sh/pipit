package main

import (
	"errors"
	"fmt"
)

type Shape interface {
	Area() int
	Scale(k int) Shape
}

type Sq struct{ s int }

func (q Sq) Area() int         { return q.s * q.s }
func (q Sq) Scale(k int) Shape { return Sq{q.s * k} }

type Named struct{ Sq }

func run() string {
	area := Shape.Area
	scale := Shape.Scale
	errText := error.Error
	var shapes = []Shape{Sq{2}, Named{Sq{3}}}
	total := 0
	for _, s := range shapes {
		total += area(s) + scale(s, 2).Area()
	}
	return fmt.Sprint(total, " ", Shape.Area(Sq{5}), " ", errText(errors.New("boom")))
}
