package main

import "fmt"

type list []int

var kept []int

var seen list

func keep(s []int) { kept = s }

func (l list) remember() { seen = l }

func returned() []int {
	var l []int
	l = append(l, 1)
	return l
}

func run() string {
	var out []string
	add := func(label string, values ...any) { out = append(out, label+"="+fmt.Sprint(values...)) }

	var b []int
	b = append(b, 1)
	a := b
	a = append(a, 2)
	add("copy", len(a), len(b))

	var names []string
	names = append(names, "x")
	other := names
	other = append(other, "y")
	add("copyStrings", len(other), len(names))

	var argument []int
	argument = append(argument, 1)
	keep(argument)
	argument = append(argument, 2)
	add("argument", len(argument), len(kept))

	var receiver list
	receiver = append(receiver, 1)
	receiver.remember()
	receiver = append(receiver, 2)
	add("receiver", len(receiver), len(seen))

	first := returned()
	second := first
	second = append(second, 2)
	add("returned", len(first), len(second))

	var tuple []int
	tuple = append(tuple, 1)
	alias, n := tuple, 5
	alias = append(alias, 2)
	add("tuple", len(tuple), len(alias), n)

	deferred := func() (result int) {
		var l []int
		l = append(l, 1)
		defer func(x []int) { result = len(x) }(l)
		l = append(l, 2)
		return 0
	}
	add("deferred", deferred())

	var paths []string
	var prefix []string
	for _, step := range []string{"a", "b", "c"} {
		prefix = append(prefix, step)
		path := prefix
		path = append(path, "leaf")
		paths = append(paths, fmt.Sprint(len(prefix), len(path), path[len(path)-1]))
	}
	add("paths", paths)
	return fmt.Sprint(out)
}
