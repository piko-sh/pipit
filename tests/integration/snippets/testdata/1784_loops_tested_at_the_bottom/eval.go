package main

import (
	"fmt"
	"strings"
)

type box struct {
	limit int
	seen  []int
}

type node struct {
	value int
	next  *node
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

func zeroTrips(n int) string {
	out := []string{}
	for i := 5; i < n; i++ {
		out = append(out, "for")
	}
	for i := range n - 5 {
		out = append(out, fmt.Sprint("range", i))
	}
	for i := 0; i < 0; i++ {
		out = append(out, "const")
	}
	for range []int(nil) {
		out = append(out, "slice")
	}
	return fmt.Sprint(len(out), out)
}

func continues(n int) int {
	total := 0
	for i := 0; i < 12; i++ {
		if i%3 == 0 {
			continue
		}
		if i > n {
			break
		}
		total += i
	}
	for i := 0; i < n; i++ {
		if i&1 == 1 {
			continue
		}
		total += 100 * i
	}
	return total
}

func nested(rows, columns int) int {
	total := 0
	for row := 0; row < rows; row++ {
		scale := row*7 + 3
		for column := 0; column < columns; column++ {
			if column == row {
				total += 1000
			}
			total += scale * column
		}
		for column := columns - 1; column >= 0; column-- {
			total -= column
		}
	}
	return total
}

func conditionCalls(limit int) string {
	calls := 0
	below := func(i int) bool {
		calls++
		return i < limit
	}
	steps := 0
	for i := 0; below(i); i++ {
		steps++
	}
	return fmt.Sprint(steps, calls)
}

func boundChangesInBody() int {
	n := 4
	total := 0
	for i := 0; i < n; i++ {
		if i == 2 {
			n = 7
		}
		total += i
	}
	return total*10 + n
}

func nilAfterStore(b *box, out []int) {
	for i := 0; i < 3; i++ {
		out[i] = i + 1
		out[i] += b.limit
	}
}

func nilInCondition(b *box, out []int) {
	for i := 0; i < b.limit; i++ {
		out[i] = 9
	}
}

func walk(head *node) int {
	total := 0
	for n := head; n != nil; n = n.next {
		total = total*10 + n.value
	}
	return total
}

func closures() string {
	var printers []func() int
	for i := 0; i < 3; i++ {
		printers = append(printers, func() int { return i * i })
	}
	for i := range 3 {
		printers = append(printers, func() int { return i + 10 })
	}
	parts := []string{}
	for _, printer := range printers {
		parts = append(parts, fmt.Sprint(printer()))
	}
	return strings.Join(parts, ",")
}

func labelled() int {
	count := 0
outer:
	for i := 0; i < 5; i++ {
		for j := 0; j < 5; j++ {
			if j == 3 {
				continue outer
			}
			if i == 4 {
				break outer
			}
			count += i*10 + j
		}
	}
	return count
}

func unsignedAndBytes(text string, values []uint16) uint64 {
	var total uint64
	for i := range uint64(len(values)) {
		total += uint64(values[i]) << (i % 8)
	}
	for _, b := range []byte(text) {
		total = total*31 + uint64(b)
	}
	for i, r := range text {
		total += uint64(i) * uint64(r)
	}
	return total
}

func run() string {
	lines := []string{
		"zero " + zeroTrips(5) + " " + zeroTrips(7),
		fmt.Sprint("continues ", continues(8), continues(0), continues(20)),
		fmt.Sprint("nested ", nested(4, 5), nested(0, 5), nested(3, 0)),
		"calls " + conditionCalls(4) + " " + conditionCalls(0),
		fmt.Sprint("bound ", boundChangesInBody()),
	}

	stored := make([]int, 3)
	lines = append(lines, "nil after store "+caught(func() { nilAfterStore(nil, stored) })+fmt.Sprint(stored))
	untouched := make([]int, 3)
	lines = append(lines, "nil in condition "+caught(func() { nilInCondition(nil, untouched) })+fmt.Sprint(untouched))
	filled := make([]int, 3)
	nilAfterStore(&box{limit: 5}, filled)
	lines = append(lines, fmt.Sprint("filled ", filled))

	list := &node{value: 1, next: &node{value: 2, next: &node{value: 3}}}
	lines = append(lines, fmt.Sprint("walk ", walk(list), walk(nil)))
	lines = append(lines, "closures "+closures())
	lines = append(lines, fmt.Sprint("labelled ", labelled()))
	lines = append(lines, fmt.Sprint("unsigned ", unsignedAndBytes("héllo", []uint16{1, 2, 3, 500, 65535})))
	return strings.Join(lines, "\n")
}
