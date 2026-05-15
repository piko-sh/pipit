package main

import "fmt"

type cell struct {
	value int
}

func run() string {
	first := &cell{value: 11}
	second := &cell{value: 22}
	third := &cell{value: 33}

	slots := make([]*cell, 0, 4)
	slots = append(slots, first)
	bound := slots[len(slots)-1]

	slots[0] = second
	slots = slots[:len(slots)-1]
	slots = append(slots, third)

	return fmt.Sprintf("%d %d %d", bound.value, slots[0].value, len(slots))
}
