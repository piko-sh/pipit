package main

type Rect struct{ W, H int }

func (r Rect) Area() int { return r.W * r.H }

func run() int {
	r := Rect{W: 5, H: 5}
	return r.Area()
}
