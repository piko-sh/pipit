package main

import "fmt"

func boxedElements() string {
	s := make([]any, 1, 8)
	var all [][]any
	all = append(all, s)
	s = append(s, 9)
	s = append(s, 10)
	return fmt.Sprint(len(all[0]), cap(all[0]), len(s))
}

func pointerElements() string {
	x := 1
	s := make([]*int, 2, 8)
	var all [][]*int
	all = append(all, s)
	s = append(s, &x)
	return fmt.Sprint(len(all[0]), len(s), all[0][1] == nil, *s[2])
}

func interfaceElements() string {
	s := make([]any, 1, 8)
	var all []any
	all = append(all, s)
	s = append(s, 9)
	s = append(s, 10)
	return fmt.Sprint(len(all[0].([]any)), len(s))
}

func loopedSnapshots() string {
	s := make([]any, 0, 16)
	var snapshots [][]any
	var boxed []any
	for i := 0; i < 4; i++ {
		snapshots = append(snapshots, s)
		boxed = append(boxed, s)
		s = append(s, i)
	}
	out := ""
	for i, snapshot := range snapshots {
		out += fmt.Sprint(len(snapshot), len(boxed[i].([]any)))
	}
	return out + fmt.Sprint(len(s))
}

func run() string {
	return boxedElements() + ";" + pointerElements() + ";" + interfaceElements() + ";" + loopedSnapshots()
}
