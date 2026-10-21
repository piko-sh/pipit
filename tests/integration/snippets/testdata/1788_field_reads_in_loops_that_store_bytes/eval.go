package main

import (
	"fmt"
	"strings"
)

type header struct {
	bias  int
	scale uint8
	name  string
	cells []byte
	ok    bool
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

func fill(h *header, out []byte) int {
	total := 0
	for i := 0; i < len(out); i++ {
		out[i] = byte(h.bias + i*int(h.scale))
		if h.ok {
			total += len(h.cells) + len(h.name)
		}
		h.cells[i%len(h.cells)] = out[i]
	}
	return total
}

func fieldChangedInside(h *header, out []byte) {
	for i := 0; i < len(out); i++ {
		out[i] = byte(h.bias)
		h.bias += 3
		if i == 2 {
			h.cells = h.cells[:1]
		}
		out[i] += byte(len(h.cells))
	}
}

func nilAfterAStore(h *header, out []byte) {
	for i := 0; i < len(out); i++ {
		out[i] = 9
		out[i] += byte(h.bias)
	}
}

func skippedOnNil(h *header, out []byte) {
	for i := 0; i < len(out); i++ {
		out[i] = byte(i)
		if h != nil {
			out[i] += h.scale
		}
	}
}

func run() string {
	h := &header{bias: 4, scale: 3, name: "ab", cells: make([]byte, 3), ok: true}
	out := make([]byte, 5)
	lines := []string{
		fmt.Sprint("fill ", fill(h, out), out, h.cells),
	}
	changed := make([]byte, 5)
	fieldChangedInside(h, changed)
	lines = append(lines, fmt.Sprint("changed inside ", changed, h.bias, len(h.cells)))

	stored := make([]byte, 3)
	lines = append(lines, "nil after a store "+caught(func() { nilAfterAStore(nil, stored) })+fmt.Sprint(stored))
	skipped := make([]byte, 3)
	skippedOnNil(nil, skipped)
	lines = append(lines, fmt.Sprint("skipped on nil ", skipped))
	skippedOnNil(h, skipped)
	lines = append(lines, fmt.Sprint("not skipped ", skipped))
	return strings.Join(lines, "\n")
}
