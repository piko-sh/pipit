package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

type label string

func (l label) String() string { return "label " + string(l) }

type plain string

type code int

func (c code) String() string { return fmt.Sprintf("code-%d", int(c)) }

type failing string

func (f failing) Error() string { return "failing " + string(f) }

type quoted string

func (q quoted) String() string { return `"` + string(q) + `"` }

func run() string {
	var buffer bytes.Buffer
	fmt.Fprint(&buffer, label("w"), 2, plain("x"), 3)
	args := []any{plain("p"), 1, label("q"), true, code(4), 5}
	lines := []string{
		fmt.Sprint(label("z"), true),
		fmt.Sprint(true, label("z")),
		fmt.Sprint(plain("p"), 1),
		fmt.Sprint(label("a"), label("b")),
		fmt.Sprint(code(7), 8),
		fmt.Sprint(failing("f"), 9, errors.New("e"), 10),
		fmt.Sprint(nil, label("n"), error(nil), 3, []int(nil), plain("m")),
		fmt.Sprint(args...),
		fmt.Sprintln(label("z"), true, plain("p")),
		fmt.Sprintf("%v|%s|%q|%x|%T|%#v", label("z"), plain("p"), quoted("q"), label("h"), plain("t"), plain("g")),
		string(fmt.Append(nil, plain("a"), 1, label("b"))),
		buffer.String(),
	}
	return strings.Join(lines, "\n")
}
