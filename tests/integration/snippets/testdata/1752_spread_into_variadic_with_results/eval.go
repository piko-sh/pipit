package main

import (
	"fmt"
	"io"
	"strings"
)

type wrapped struct{ v any }

func (w *wrapped) Format(state fmt.State, verb rune) { fmt.Fprintf(state, "<%v>", w.v) }

func convertArgs(args []any) (formatters []any) {
	formatters = make([]any, len(args))
	for i, arg := range args {
		formatters[i] = &wrapped{arg}
	}
	return formatters
}

func sprintfVia(format string, a ...any) string {
	return fmt.Sprintf(format, convertArgs(a)...)
}

func fprintfVia(w io.Writer, format string, a ...any) (int, error) {
	return fmt.Fprintf(w, format, convertArgs(a)...)
}

type counter struct{ base int }

func (c counter) sum(xs ...int) (int, int) {
	total := c.base
	for _, x := range xs {
		total += x
	}
	return len(xs), total
}

type summer interface {
	sum(xs ...int) (int, int)
}

func pair(xs ...int) (int, int) { return len(xs), xs[len(xs)-1] }

func run() string {
	var out strings.Builder
	args := []any{1, "two"}
	n, err := fmt.Fprintf(&out, "native %v %v\n", args...)
	fmt.Fprintln(&out, n, err)

	fprintf := fmt.Fprintf
	n, err = fprintf(&out, "value %v %v\n", args...)
	fmt.Fprintln(&out, n, err)

	n, err = fprintfVia(&out, "formatter %v %#+v\n", 4, []string{"a"})
	fmt.Fprintln(&out, n, err)
	fmt.Fprintln(&out, sprintfVia("%v %v", 5, "six"))

	var x, y int
	targets := []any{&x, &y}
	n, err = fmt.Sscan("10 20", targets...)
	fmt.Fprintln(&out, n, err, x, y)
	n, err = fmt.Sscanf("30-40", "%d-%d", targets...)
	fmt.Fprintln(&out, n, err, x, y)

	ints := []int{3, 4, 5}
	closure := func(xs ...int) (int, int) { return len(xs), xs[0] }
	a, b := closure(ints...)
	fmt.Fprintln(&out, "closure", a, b)

	a, b = pair(ints...)
	fmt.Fprintln(&out, "func", a, b)

	c := counter{base: 10}
	a, b = c.sum(ints...)
	fmt.Fprintln(&out, "method", a, b)

	var s summer = c
	a, b = s.sum(ints...)
	fmt.Fprintln(&out, "interface", a, b)
	a, b = s.sum(1, 2)
	fmt.Fprintln(&out, "interface packed", a, b)

	method := c.sum
	a, b = method(ints...)
	fmt.Fprintln(&out, "method value", a, b)

	return out.String()
}
