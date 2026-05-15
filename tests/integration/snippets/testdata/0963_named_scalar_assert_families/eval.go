package main

import "fmt"

type Word string
type Celsius float64
type Count int
type Flag bool
type Mask uint32

func run() string {
	var w any = Word("answer")
	var c any = Celsius(20)
	var n any = Count(7)
	var f any = Flag(true)
	var m any = Mask(6)

	_, wPlain := w.(string)
	_, wNamed := w.(Word)
	_, cPlain := c.(float64)
	_, cNamed := c.(Celsius)
	_, nPlain := n.(int)
	_, nNamed := n.(Count)
	_, fPlain := f.(bool)
	_, fNamed := f.(Flag)
	_, mPlain := m.(uint32)
	_, mNamed := m.(Mask)

	var s any = "answer"
	_, sNamed := s.(Word)
	_, sPlain := s.(string)

	return fmt.Sprintf("%v%v %v%v %v%v %v%v %v%v %v%v",
		wPlain, wNamed, cPlain, cNamed, nPlain, nNamed,
		fPlain, fNamed, mPlain, mNamed, sNamed, sPlain)
}
