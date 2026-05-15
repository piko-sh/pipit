package main

import "fmt"

type row []int

type stack []int

func (s stack) depth() int {
	return len(s)
}

type inner struct {
	values []int
}

type outer struct {
	nested inner
	cells  row
	ops    stack
}

type parser struct {
	source []byte
	name   string
}

func run() string {
	empty := &parser{name: "empty"}
	nilFieldLen := len(empty.source)

	populated := &parser{source: []byte("abcdef")}
	pointerLen := len(populated.source)

	populated.source = populated.source[:2]
	resliced := len(populated.source)

	byValue := outer{
		nested: inner{values: []int{1, 2, 3}},
		cells:  row{4, 5},
		ops:    stack{6, 7, 8},
	}
	nestedLen := len(byValue.nested.values)
	namedLen := len(byValue.cells)
	methodNamedLen := len(byValue.ops) + byValue.ops.depth()

	snapshot := byValue
	byValue.nested.values = append(byValue.nested.values, 9, 10)
	copyLen := len(snapshot.nested.values)
	grownLen := len(byValue.nested.values)

	return fmt.Sprintf("%d %d %d %d %d %d %d %d",
		nilFieldLen, pointerLen, resliced, nestedLen, namedLen, methodNamedLen, copyLen, grownLen)
}
