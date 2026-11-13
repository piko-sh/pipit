package main

import (
	"fmt"
	"strings"
)

type view struct {
	data   []byte
	offset int
}

func (v view) at(i int) byte {
	index := v.offset + i
	if index >= len(v.data) && index < len(v.data)+4 {
		return 0
	}
	return v.data[index]
}

type holder struct {
	data   []byte
	names  []string
	counts []int
	offset int
	scale  int32
}

var source view
var screen []byte

func caught(body func()) (message string) {
	defer func() {
		if r := recover(); r != nil {
			message = fmt.Sprint(r)
		}
	}()
	body()
	return "no panic"
}

func column(count, step int32) {
	var frac int32
	dest := 0
	for ; count >= 0; count-- {
		screen[dest] = source.at(int(frac >> 16 & 7))
		dest += 2
		frac += step
	}
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

func growing(h *holder, n int) int {
	total := 0
	for i := 0; i < n; i++ {
		h.data = append(h.data, byte(i))
		total += len(h.data) + int(h.data[len(h.data)-1])
	}
	return total
}

func throughAnAlias(h, alias *holder, n int) int {
	total := 0
	for i := 0; i < n; i++ {
		alias.offset++
		alias.data = alias.data[1:]
		total += h.offset*100 + len(h.data)
	}
	return total
}

func reassigned(h *holder, other *holder, n int) int {
	total := 0
	for i := 0; i < n; i++ {
		if i == 2 {
			h = other
		}
		total += h.offset + len(h.names)
	}
	return total
}

func onlyOnAnArm(h *holder, n int) int {
	total := 0
	for i := 0; i < n; i++ {
		if i > 100 {
			total += h.offset + len(h.counts)
		}
		total++
	}
	return total
}

func afterAStore(h *holder, out []int, n int) (written int) {
	for i := 0; i < n; i++ {
		out[i] = i + 1
		written++
		out[i] += h.offset + len(h.names)
	}
	return written
}

func shifted(h *holder, amount int, n int) int {
	total := 0
	for i := 0; i < n; i++ {
		total += (i << amount) + int(h.scale) + len(h.counts)
	}
	return total
}

func typedSlices(h *holder, n int) string {
	parts := []string{}
	sum := 0
	for i := 0; i < n; i++ {
		parts = append(parts, h.names[i%len(h.names)])
		sum += h.counts[i%len(h.counts)]
	}
	return fmt.Sprint(strings.Join(parts, ""), sum)
}

func zeroTripsRunNothing() bool {
	var none *holder
	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		throughPointer(none, 0)
		onlyOnAnArm(none, 0)
		shifted(none, 1, 0)
		typedSlices(none, 0)
	}()
	return !panicked
}

func run() string {
	source = view{data: []byte{10, 11, 12, 13, 14, 15}, offset: 2}
	screen = make([]byte, 16)
	column(7, 1<<16)
	h := &holder{data: []byte{1, 2, 3, 4, 5}, names: []string{"a", "b", "c"}, counts: []int{7, 8}, offset: 1, scale: 3}
	other := &holder{names: []string{"x"}, offset: 40}
	var none *holder
	out := make([]int, 3)
	lines := []string{
		fmt.Sprint(screen),
		fmt.Sprint(throughPointer(h, 6), " ", throughPointer(h, 0)),
		caught(func() { throughPointer(none, 3) }),
		fmt.Sprint("zero trips run nothing: ", zeroTripsRunNothing()),
		fmt.Sprint(growing(&holder{}, 5)),
		fmt.Sprint(throughAnAlias(h, h, 3), " ", h.offset, " ", h.data),
		fmt.Sprint(reassigned(h, other, 5)),
		fmt.Sprint(onlyOnAnArm(none, 4), " ", onlyOnAnArm(h, 103)),
		caught(func() { onlyOnAnArm(none, 103) }),
		fmt.Sprint(afterAStore(h, out, 3), " ", out),
	}
	partial := make([]int, 2)
	lines = append(lines,
		caught(func() { afterAStore(h, partial, 3) })+fmt.Sprint(" ", partial),
		caught(func() { afterAStore(none, partial, 1) })+fmt.Sprint(" ", partial),
		fmt.Sprint(shifted(h, 2, 4), " ", shifted(h, 0, 0)),
		caught(func() { shifted(none, 1, 2) }),
		typedSlices(h, 5),
		caught(func() { typedSlices(none, 1) }),
	)
	return strings.Join(lines, "\n")
}
