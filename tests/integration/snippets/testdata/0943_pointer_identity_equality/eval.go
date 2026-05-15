package main

import "fmt"

type node struct {
	value int
	next  *node
}

func run() string {
	a := &node{value: 1}
	b := &node{value: 1}
	c := a

	sameObject := a == c
	differentObjects := a == b

	var pNil *node
	var qNil *node
	nilEqual := pNil == qNil
	nilVsLive := pNil == a

	var ai any = a
	var ci any = c
	var bi any = b
	ifaceSame := ai == ci
	ifaceDifferent := ai == bi

	ch1 := make(chan int, 1)
	ch2 := make(chan int, 1)
	ch3 := ch1
	chanSame := ch1 == ch3
	chanDifferent := ch1 == ch2

	a.next = b
	linkSame := a.next == b

	return fmt.Sprintf("%v %v %v %v %v %v %v %v %v",
		sameObject, differentObjects, nilEqual, nilVsLive,
		ifaceSame, ifaceDifferent, chanSame, chanDifferent, linkSame)
}
