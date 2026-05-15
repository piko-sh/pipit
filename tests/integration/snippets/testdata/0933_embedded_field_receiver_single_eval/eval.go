package main

import "fmt"

type Base struct {
	Label string
	Age   int
}

type Outer struct {
	*Base
}

var (
	calls    int
	shared   = Outer{Base: &Base{}}
	arrCalls int
	arr      = []Outer{{Base: &Base{}}, {Base: &Base{}}}
)

func acquire() *Outer {
	calls++
	return &shared
}

func pick() []Outer {
	arrCalls++
	return arr
}

func run() string {
	acquire().Label = "hi"
	acquire().Age += 5
	acquire().Age++

	pick()[1].Label = "index"
	pick()[1].Age += 2
	pick()[1].Age--

	return fmt.Sprintf("calls=%d label=%s age=%d arrCalls=%d arrLabel=%s arrAge=%d",
		calls, shared.Label, shared.Age, arrCalls, arr[1].Label, arr[1].Age)
}
