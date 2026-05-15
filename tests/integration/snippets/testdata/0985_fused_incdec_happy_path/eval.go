package main

import "fmt"

type tallies struct {
	signed   int
	unsigned uint
}

func (t *tallies) bumpThroughPointer() {
	t.signed++
	t.signed++
	t.signed--
	t.unsigned++
	t.unsigned++
}

func run() string {
	viaPointer := &tallies{signed: 10, unsigned: 3}
	viaPointer.bumpThroughPointer()

	local := tallies{signed: 100, unsigned: 50}
	local.signed++
	local.unsigned--

	accumulator := &tallies{}
	for iteration := 0; iteration < 1000; iteration++ {
		accumulator.signed++
		accumulator.unsigned++
	}

	return fmt.Sprintf("%d %d %d %d %d %d",
		viaPointer.signed, viaPointer.unsigned,
		local.signed, local.unsigned,
		accumulator.signed, accumulator.unsigned)
}
