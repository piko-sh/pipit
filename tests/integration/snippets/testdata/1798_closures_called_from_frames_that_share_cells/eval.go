package main

import (
	"fmt"
	"strings"
)

type holder struct {
	data   []byte
	offset int
}

func caught(body func()) (message string) {
	defer func() {
		if r := recover(); r != nil {
			message = fmt.Sprint(r)
		}
	}()
	body()
	return "no panic"
}

func throughPointer(h *holder, n int) int {
	total := 0
	for i := 0; i < n; i++ {
		index := h.offset + i
		if index < len(h.data) {
			total += int(h.data[index])
		}
	}
	return total
}

func counted(body func()) (calls int) {
	defer func() { calls++ }()
	body()
	return calls
}

func shared() string {
	count := 0
	bump := func() { count++ }
	label := "start"
	rename := func() { label = "renamed" }
	bump()
	counted(bump)
	rename()
	return fmt.Sprint(count, " ", label)
}

func run() string {
	h := &holder{data: []byte{1, 2, 3, 4, 5}, offset: 1}
	var none *holder
	first := throughPointer(h, 6)
	lines := []string{
		fmt.Sprint(first),
		caught(func() { throughPointer(none, 3) }),
		"zero trips: " + caught(func() { throughPointer(none, 0) }),
		fmt.Sprint(counted(func() { throughPointer(none, 0) })),
		shared(),
	}
	return strings.Join(lines, "\n")
}
